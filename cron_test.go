package cartridge

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karloscodes/cartridge/query"
	"github.com/karloscodes/cartridge/sqlite"
)

func TestCronSchedule(t *testing.T) {
	at := func(value string) time.Time {
		parsed, err := time.Parse("2006-01-02 15:04:05", value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	// 10 October 2026 is a Saturday.
	cases := []struct {
		name, spec, after, want string
	}{
		{"daily, before the time", "0 8 * * *", "2026-10-10 07:59:30", "2026-10-10 08:00:00"},
		{"daily, at the time", "0 8 * * *", "2026-10-10 08:00:00", "2026-10-11 08:00:00"},
		{"a step", "*/15 * * * *", "2026-10-10 08:16:00", "2026-10-10 08:30:00"},
		{"a step from a start", "5/20 * * * *", "2026-10-10 08:26:00", "2026-10-10 08:45:00"},
		{"the first of the month", "0 0 1 * *", "2026-10-10 12:00:00", "2026-11-01 00:00:00"},
		{"weekdays", "0 9 * * 1-5", "2026-10-10 12:00:00", "2026-10-12 09:00:00"},
		{"Sunday as 7", "0 9 * * 7", "2026-10-10 12:00:00", "2026-10-11 09:00:00"},
		{"a list", "0 6,18 * * *", "2026-10-10 07:00:00", "2026-10-10 18:00:00"},
		{"day of month or day of week", "0 0 1,15 * 1", "2026-10-10 12:00:00", "2026-10-12 00:00:00"},
		{"a leap day", "30 2 29 2 *", "2026-10-10 12:00:00", "2028-02-29 02:30:00"},
		{"the end of a year", "0 0 1 1 *", "2026-10-10 12:00:00", "2027-01-01 00:00:00"},
		{"@daily", "@daily", "2026-10-10 12:00:00", "2026-10-11 00:00:00"},
		{"@hourly", "@hourly", "2026-10-10 12:30:00", "2026-10-10 13:00:00"},
		{"@every", "@every 90s", "2026-10-10 12:00:00", "2026-10-10 12:01:30"},
		{"a time zone in summer", "TZ=Europe/Madrid 0 8 * * *", "2026-10-10 05:00:00", "2026-10-10 06:00:00"},
		{"a time zone in winter", "TZ=Europe/Madrid 0 8 * * *", "2026-12-10 05:00:00", "2026-12-10 07:00:00"},
		{"a zone with a half-hour offset", "TZ=Asia/Kolkata 0 9 * * *", "2026-10-10 00:00:00", "2026-10-10 03:30:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sched, err := parseSchedule(c.spec)
			if err != nil {
				t.Fatalf("parse %q: %v", c.spec, err)
			}

			got := sched.next(at(c.after)).UTC()

			if !got.Equal(at(c.want)) {
				t.Errorf("%q after %s: next = %s, want %s", c.spec, c.after, got.Format("2006-01-02 15:04:05"), c.want)
			}
		})
	}

	t.Run("a wrong schedule is an error", func(t *testing.T) {
		scheduler := NewCronScheduler(testLogger(), &testDBManager{})
		job := func(*JobContext) error { return nil }
		for _, spec := range []string{
			"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
			"*/0 * * * *", "5-1 * * * *", "a * * * *", "0 0 30 2 *", "@every -1s", "@every soon", "@sometimes",
			"TZ=Nowhere/City 0 8 * * *", "TZ= 0 8 * * *",
		} {
			if err := scheduler.Add("job", spec, job); err == nil {
				t.Errorf("spec %q: Add returned nil, want an error", spec)
			}
		}
	})

	t.Run("a wrong or repeated name is an error", func(t *testing.T) {
		scheduler := NewCronScheduler(testLogger(), &testDBManager{})
		job := func(*JobContext) error { return nil }
		if err := scheduler.Add("send-digest", "@daily", job); err != nil {
			t.Fatal(err)
		}

		for _, name := range []string{"", "Send Digest", "it's", "send-digest"} {
			if err := scheduler.Add(name, "@daily", job); err == nil {
				t.Errorf("name %q: Add returned nil, want an error", name)
			}
		}
	})
}

// newCronManager returns a SQLite database for schedule state.
func newCronManager(t *testing.T) *sqlite.Manager {
	t.Helper()
	m := sqlite.NewManager(sqlite.Config{Path: filepath.Join(t.TempDir(), "cron.db"), Logger: testLogger()})
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func cronSQL(t *testing.T, m DBManager) *sql.DB {
	t.Helper()
	db, err := m.Connect()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	return sqlDB
}

func startCron(t *testing.T, m DBManager, name, spec string, fn CronFunc) *CronScheduler {
	t.Helper()
	scheduler := NewCronScheduler(testLogger(), m)
	if err := scheduler.Add(name, spec, fn); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scheduler.Stop)
	return scheduler
}

func TestCronScheduler(t *testing.T) {
	t.Run("runs a job again and again, with the app's database", func(t *testing.T) {
		m := newCronManager(t)
		var runs atomic.Int32
		var sawDB atomic.Bool

		startCron(t, m, "tick", "@every 20ms", func(ctx *JobContext) error {
			sawDB.Store(ctx.DB != nil)
			runs.Add(1)
			return nil
		})
		time.Sleep(300 * time.Millisecond)

		if runs.Load() < 3 || !sawDB.Load() {
			t.Errorf("runs = %d, saw the database = %v, want several runs", runs.Load(), sawDB.Load())
		}
	})

	t.Run("two schedulers on one database run each tick once", func(t *testing.T) {
		m := newCronManager(t)
		var runs atomic.Int32
		job := func(*JobContext) error { runs.Add(1); return nil }

		begin := time.Now()
		first := startCron(t, m, "tick", "@every 100ms", job)
		second := startCron(t, m, "tick", "@every 100ms", job)
		time.Sleep(650 * time.Millisecond)
		first.Stop()
		second.Stop()

		ticks := int32(time.Since(begin)/(100*time.Millisecond)) + 1
		if got := runs.Load(); got < 3 || got > ticks {
			t.Errorf("runs = %d, want 3 to %d: one run per tick, not one per scheduler", got, ticks)
		}
	})

	t.Run("a run that was due while the app was down happens once at the start", func(t *testing.T) {
		m := newCronManager(t)
		var runs atomic.Int32
		job := func(*JobContext) error { runs.Add(1); return nil }
		startCron(t, m, "nightly", "@daily", job).Stop()
		if _, err := cronSQL(t, m).Exec("UPDATE cartridge_cron SET next_run_at = 1 WHERE name = 'nightly'"); err != nil {
			t.Fatal(err)
		}

		startCron(t, m, "nightly", "@daily", job)
		time.Sleep(300 * time.Millisecond)

		if runs.Load() != 1 {
			t.Errorf("runs = %d, want 1", runs.Load())
		}
	})

	t.Run("a job waits for its schedule, also after a restart", func(t *testing.T) {
		m := newCronManager(t)
		var runs atomic.Int32
		job := func(*JobContext) error { runs.Add(1); return nil }

		startCron(t, m, "nightly", "@daily", job).Stop()
		startCron(t, m, "nightly", "@daily", job)
		time.Sleep(150 * time.Millisecond)

		if runs.Load() != 0 {
			t.Errorf("runs = %d, want 0 before the schedule", runs.Load())
		}
	})

	t.Run("a new schedule for a known job replaces the stored next run", func(t *testing.T) {
		m := newCronManager(t)
		var runs atomic.Int32
		job := func(*JobContext) error { runs.Add(1); return nil }
		startCron(t, m, "report", "@yearly", job).Stop()

		startCron(t, m, "report", "@every 20ms", job)
		time.Sleep(200 * time.Millisecond)

		if runs.Load() < 2 {
			t.Errorf("runs = %d, want the new schedule to run", runs.Load())
		}
	})

	t.Run("a panic is stored as the last error, and the job runs again", func(t *testing.T) {
		m := newCronManager(t)
		var runs atomic.Int32
		panicked := make(chan struct{})
		proceed := make(chan struct{})

		scheduler := startCron(t, m, "fragile", "@every 20ms", func(*JobContext) error {
			switch runs.Add(1) {
			case 1:
				panic("boom")
			case 2:
				close(panicked)
				<-proceed
			}
			return nil
		})
		<-panicked
		duringSecond, err := query.One[*string](context.Background(), cronSQL(t, m), "SELECT last_error FROM cartridge_cron WHERE name = 'fragile'")
		close(proceed)
		time.Sleep(100 * time.Millisecond)
		scheduler.Stop()
		afterGoodRun, _ := query.One[*string](context.Background(), cronSQL(t, m), "SELECT last_error FROM cartridge_cron WHERE name = 'fragile'")

		if err != nil || duringSecond == nil || *duringSecond != "panic: boom" {
			t.Errorf("last error after the panic = %v (%v), want \"panic: boom\"", duringSecond, err)
		}
		if afterGoodRun != nil {
			t.Errorf("last error after a good run = %q, want none", *afterGoodRun)
		}
	})

	t.Run("Stop cancels a running job and waits for it", func(t *testing.T) {
		m := newCronManager(t)
		started, ended := make(chan struct{}), make(chan struct{})

		scheduler := startCron(t, m, "long", "@every 10ms", func(ctx *JobContext) error {
			close(started)
			<-ctx.Done()
			close(ended)
			return ctx.Err()
		})
		<-started
		scheduler.Stop()

		select {
		case <-ended:
		default:
			t.Error("Stop returned before the job ended")
		}
	})
}

func TestWithCron(t *testing.T) {
	job := func(*JobContext) error { return nil }

	t.Run("an app runs its cron jobs, and Shutdown stops them", func(t *testing.T) {
		var runs atomic.Int32
		app, err := NewApp(newAppTestConfig(t), WithCron("tick", "@every 20ms", func(*JobContext) error { runs.Add(1); return nil }))
		if err != nil {
			t.Fatal(err)
		}
		if err := app.startWorkers(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)

		_ = app.Shutdown(context.Background())
		atStop := runs.Load()
		time.Sleep(100 * time.Millisecond)

		if atStop < 2 || runs.Load() != atStop {
			t.Errorf("runs = %d at Shutdown and %d later, want several and then no more", atStop, runs.Load())
		}
	})

	t.Run("WithCronDatabase keeps the state in the named database", func(t *testing.T) {
		app, err := NewApp(newAppTestConfig(t),
			WithDatabase("shared", sqlite.Config{Path: filepath.Join(t.TempDir(), "shared.db")}),
			WithCronDatabase("shared"),
			WithCron("tick", "@daily", job))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Shutdown(context.Background()) })

		if err := app.startWorkers(); err != nil {
			t.Fatal(err)
		}

		ctx := context.Background()
		inShared, sharedErr := query.One[int](ctx, cronSQL(t, app.Databases["shared"]), "SELECT COUNT(*) FROM cartridge_cron")
		_, mainErr := query.One[int](ctx, cronSQL(t, app.DBManager), "SELECT COUNT(*) FROM cartridge_cron")
		if sharedErr != nil || inShared != 1 || mainErr == nil {
			t.Errorf("shared has %d jobs (%v), main table exists = %v, want the state only in shared", inShared, sharedErr, mainErr == nil)
		}
	})

	t.Run("a wrong schedule, a repeated name, or an unknown database fails NewApp", func(t *testing.T) {
		for name, opts := range map[string][]AppOption{
			"wrong schedule":   {WithCron("tick", "not a schedule", job)},
			"repeated name":    {WithCron("tick", "@daily", job), WithCron("tick", "@hourly", job)},
			"unknown database": {WithCron("tick", "@daily", job), WithCronDatabase("nowhere")},
			"database alone":   {WithCronDatabase("nowhere")},
		} {
			if _, err := NewApp(newAppTestConfig(t), opts...); err == nil {
				t.Errorf("%s: NewApp returned nil, want an error", name)
			}
		}
	})
}
