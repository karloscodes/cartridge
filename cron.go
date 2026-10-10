package cartridge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/karloscodes/cartridge/internal/dialect"
)

// CronFunc is the work of a cron job.
type CronFunc func(ctx *JobContext) error

// CronScheduler runs jobs on schedules. It keeps the next run of each job
// in the table cartridge_cron, so:
//
//   - a restart does not lose or repeat a run: a run that was due while the
//     app was down happens once at the start;
//   - several processes on one database run each tick once. The table can
//     be in the app's database or in another one that the processes share.
//
// A job never overlaps itself: its next run waits for the current one. A
// panic or an error in a job is logged and stored, and the job runs again
// at its next time.
type CronScheduler struct {
	// Databases holds more databases by name, for JobContext.Database.
	Databases map[string]DBManager

	logger Logger
	db     DBManager // the database that jobs get
	state  DBManager // the database with cartridge_cron
	jobs   []cronJob

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type cronJob struct {
	name     string
	spec     string
	schedule schedule
	fn       CronFunc
}

// NewCronScheduler creates a scheduler. Jobs get db, and the schedule state
// is stored in db until StoreIn names another database.
func NewCronScheduler(logger Logger, db DBManager) *CronScheduler {
	return &CronScheduler{logger: logger, db: db, state: db}
}

// StoreIn keeps the schedule state in another database, for example one
// that several servers share.
func (s *CronScheduler) StoreIn(state DBManager) {
	s.state = state
}

// Add registers a job. The name identifies the job in the database: keep it
// when the schedule changes. It holds a-z, 0-9, "_", "-", and ".". The spec
// is one of:
//
//	"30 8 * * 1-5"   minute, hour, day of month, month, day of week
//	"@hourly", "@daily", "@weekly", "@monthly", "@yearly"
//	"@every 30s"     a fixed pause between the starts of two runs
//
// A field takes "*", a number, a range "1-5", a list "1,15", and a step
// "*/15". Day of week is 0-6 from Sunday; 7 is also Sunday. When both day
// fields are set, either one matches. Times are in UTC; put "TZ=<zone> " in
// front for another zone: "TZ=Europe/Madrid 0 8 * * *".
func (s *CronScheduler) Add(name, spec string, fn CronFunc) error {
	if name == "" || len(name) > 191 || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789_-.") != "" {
		return fmt.Errorf("cartridge: cron job name %q: use a-z, 0-9, \"_\", \"-\", and \".\"", name)
	}
	for _, job := range s.jobs {
		if job.name == name {
			return fmt.Errorf("cartridge: two cron jobs have the name %q", name)
		}
	}
	if fn == nil {
		return fmt.Errorf("cartridge: cron job %q has no function", name)
	}
	sched, err := parseSchedule(spec)
	if err != nil {
		return fmt.Errorf("cartridge: cron job %q: %w", name, err)
	}
	if sched.next(time.Now()).IsZero() {
		return fmt.Errorf("cartridge: cron job %q: the schedule %q never runs", name, spec)
	}
	s.jobs = append(s.jobs, cronJob{name: name, spec: spec, schedule: sched, fn: fn})
	return nil
}

// Start creates the state table and starts one loop per job.
func (s *CronScheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil || len(s.jobs) == 0 {
		return nil
	}

	gormDB, err := s.state.Connect()
	if err != nil {
		return fmt.Errorf("cartridge: cron: %w", err)
	}
	db, err := gormDB.DB()
	if err != nil {
		return fmt.Errorf("cartridge: cron: %w", err)
	}
	store := cronStore{db: db, dialect: dialect.Of(db)}
	if err := store.createTable(); err != nil {
		return fmt.Errorf("cartridge: cron: %w", err)
	}
	for _, job := range s.jobs {
		if err := store.register(job, time.Now()); err != nil {
			return fmt.Errorf("cartridge: cron job %q: %w", job.name, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	for _, job := range s.jobs {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.loop(ctx, store, job)
		}()
	}
	s.logger.Info("cron scheduler started", "jobs", len(s.jobs))
	return nil
}

// Stop ends the loops. It cancels the context of a running job and waits
// for the job to return.
func (s *CronScheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	s.wg.Wait()
	s.logger.Info("cron scheduler stopped")
}

// maxCronSleep is the longest sleep before the loop reads the state again,
// so it sees a change that another process made.
const maxCronSleep = time.Minute

func (s *CronScheduler) loop(ctx context.Context, store cronStore, job cronJob) {
	for {
		due, err := store.nextRun(ctx, job.name)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Error("cron: cannot read the schedule", "job", job.name, "error", err)
			if !sleep(ctx, 5*time.Second) {
				return
			}
			continue
		}

		if wait := time.Until(due); wait > 0 {
			if !sleep(ctx, min(wait, maxCronSleep)) {
				return
			}
			if wait > maxCronSleep {
				continue
			}
		}

		// Only one process moves the next run forward, and that one runs the job.
		now := time.Now()
		claimed, err := store.claim(ctx, job.name, due, job.schedule.next(now), now)
		if err != nil || !claimed {
			if !sleep(ctx, 50*time.Millisecond) {
				return
			}
			continue
		}

		runErr := s.run(ctx, job)
		if runErr != nil {
			s.logger.Error("cron job failed", "job", job.name, "error", runErr)
		}
		if err := store.finish(job.name, time.Now(), runErr); err != nil {
			s.logger.Error("cron: cannot store the result", "job", job.name, "error", err)
		}
	}
}

// run calls the job. A panic becomes an error.
func (s *CronScheduler) run(ctx context.Context, job cronJob) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	db, err := s.db.Connect()
	if err != nil {
		return err
	}
	return job.fn(&JobContext{
		Context:   ctx,
		Logger:    s.logger,
		DB:        db.WithContext(ctx),
		dbManager: s.db,
		databases: s.Databases,
	})
}

// sleep waits for d. It returns false when ctx ends first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// cronStore reads and writes the table cartridge_cron. Times are Unix
// milliseconds.
type cronStore struct {
	db      *sql.DB
	dialect dialect.Dialect
}

func (c cronStore) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, c.dialect.Rebind(query), args...)
}

func (c cronStore) createTable() error {
	_, err := c.db.Exec(`CREATE TABLE IF NOT EXISTS cartridge_cron (
		name VARCHAR(191) PRIMARY KEY,
		spec VARCHAR(255) NOT NULL,
		next_run_at BIGINT NOT NULL,
		last_started_at BIGINT,
		last_finished_at BIGINT,
		last_error TEXT)`)
	return err
}

// register stores a new job with its first run. For a known job whose spec
// changed, it stores the spec and the next run of the new schedule.
func (c cronStore) register(job cronJob, now time.Time) error {
	ctx := context.Background()
	next := job.schedule.next(now).UnixMilli()

	var spec string
	err := c.db.QueryRowContext(ctx, c.dialect.Rebind("SELECT spec FROM cartridge_cron WHERE name = ?"), job.name).Scan(&spec)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = c.exec(ctx, "INSERT INTO cartridge_cron (name, spec, next_run_at) VALUES (?, ?, ?)", job.name, job.spec, next)
		if dialect.IsDuplicate(err) {
			return nil // another process stored it first
		}
		return err
	}
	if err != nil || spec == job.spec {
		return err
	}
	_, err = c.exec(ctx, "UPDATE cartridge_cron SET spec = ?, next_run_at = ? WHERE name = ?", job.spec, next, job.name)
	return err
}

func (c cronStore) nextRun(ctx context.Context, name string) (time.Time, error) {
	var ms int64
	err := c.db.QueryRowContext(ctx, c.dialect.Rebind("SELECT next_run_at FROM cartridge_cron WHERE name = ?"), name).Scan(&ms)
	return time.UnixMilli(ms), err
}

// claim moves the next run from due to next. It reports whether this
// process made the change: another process that saw the same due time gets
// false.
func (c cronStore) claim(ctx context.Context, name string, due, next, now time.Time) (bool, error) {
	result, err := c.exec(ctx,
		"UPDATE cartridge_cron SET next_run_at = ?, last_started_at = ? WHERE name = ? AND next_run_at = ?",
		next.UnixMilli(), now.UnixMilli(), name, due.UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// finish stores the end of a run. It does not use the job's context, so
// the result is stored also when the scheduler stops.
func (c cronStore) finish(name string, now time.Time, runErr error) error {
	var message any
	if runErr != nil {
		message = runErr.Error()
	}
	_, err := c.exec(context.Background(),
		"UPDATE cartridge_cron SET last_finished_at = ?, last_error = ? WHERE name = ?", now.UnixMilli(), message, name)
	return err
}

// schedule says when a job runs.
type schedule struct {
	every time.Duration // set for "@every"

	// Bit n is set when the value n matches.
	minute, hour, dom, month, dow uint64
	domAny, dowAny                bool
	loc                           *time.Location
}

var cronShortcuts = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

func parseSchedule(spec string) (schedule, error) {
	s := schedule{loc: time.UTC}
	spec = strings.TrimSpace(spec)
	if rest, ok := strings.CutPrefix(spec, "TZ="); ok {
		zone, fields, _ := strings.Cut(rest, " ")
		loc, err := time.LoadLocation(zone)
		if err != nil || zone == "" {
			return s, fmt.Errorf("schedule %q: unknown time zone %q", spec, zone)
		}
		s.loc = loc
		spec = strings.TrimSpace(fields)
	}

	if d, ok := strings.CutPrefix(spec, "@every "); ok {
		every, err := time.ParseDuration(strings.TrimSpace(d))
		if err != nil || every <= 0 {
			return s, fmt.Errorf("schedule %q: want a duration above zero, such as \"@every 30s\"", spec)
		}
		s.every = every
		return s, nil
	}
	if full, ok := cronShortcuts[spec]; ok {
		spec = full
	}

	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return s, fmt.Errorf("schedule %q: want 5 fields (minute hour day month weekday), \"@every <duration>\", or a shortcut such as \"@daily\"", spec)
	}
	limits := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	var sets [5]uint64
	for i, field := range fields {
		set, err := parseCronField(field, limits[i][0], limits[i][1])
		if err != nil {
			return s, fmt.Errorf("schedule %q: field %d: %w", spec, i+1, err)
		}
		sets[i] = set
	}
	s.minute, s.hour, s.dom, s.month, s.dow = sets[0], sets[1], sets[2], sets[3], sets[4]
	if s.dow&(1<<7) != 0 { // 7 is Sunday too
		s.dow = s.dow&^(1<<7) | 1
	}
	s.domAny, s.dowAny = fields[2] == "*", fields[4] == "*"
	return s, nil
}

// parseCronField reads "*", "5", "1-5", "*/15", "1-30/2", and lists of
// these, and returns the set of values.
func parseCronField(field string, low, high int) (uint64, error) {
	var set uint64
	for _, part := range strings.Split(field, ",") {
		values, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			var err error
			if step, err = strconv.Atoi(stepText); err != nil || step < 1 {
				return 0, fmt.Errorf("step %q is not a number above zero", stepText)
			}
		}

		from, to := low, high
		if values != "*" {
			first, last, isRange := strings.Cut(values, "-")
			var err error
			if from, err = strconv.Atoi(first); err != nil {
				return 0, fmt.Errorf("%q is not a number", first)
			}
			to = from
			if isRange {
				if to, err = strconv.Atoi(last); err != nil {
					return 0, fmt.Errorf("%q is not a number", last)
				}
			} else if hasStep {
				to = high // "5/15" means from 5, every 15
			}
		}
		if from < low || to > high || from > to {
			return 0, fmt.Errorf("%q is outside %d-%d", part, low, high)
		}
		for v := from; v <= to; v += step {
			set |= 1 << v
		}
	}
	if bits.OnesCount64(set) == 0 {
		return 0, fmt.Errorf("%q matches nothing", field)
	}
	return set, nil
}

// next returns the first run after the given time. It returns the zero
// time for a schedule that has no run in the next five years, such as
// 30 February.
func (s schedule) next(after time.Time) time.Time {
	if s.every > 0 {
		return after.Add(s.every)
	}
	t := after.In(s.loc).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case s.month&(1<<int(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, s.loc)
		case !s.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, s.loc)
		case s.hour&(1<<t.Hour()) == 0:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, s.loc)
		case s.minute&(1<<t.Minute()) == 0:
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}

// dayMatches follows cron: when both the day of month and the day of week
// are set, either one matches.
func (s schedule) dayMatches(t time.Time) bool {
	dom := s.dom&(1<<t.Day()) != 0
	dow := s.dow&(1<<int(t.Weekday())) != 0
	switch {
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	}
	return dom || dow
}
