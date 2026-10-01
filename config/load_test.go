package config

import (
	"os"
	"path/filepath"
	"testing"
)

// inTempDir runs the test in an empty working directory, so Load reads
// only the .env file the test writes.
func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func writeDotEnv(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(".env", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaults(t *testing.T) {
	inTempDir(t)
	t.Setenv("PINAPP_ENV", "test")

	cfg, err := Load("pinapp")

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		AppName:          "pinapp",
		Environment:      Test,
		Port:             "8080",
		Debug:            false,
		LogLevel:         "info",
		LogsDirectory:    "storage/logs",
		LogsMaxSizeMB:    20,
		LogsMaxBackups:   10,
		LogsMaxAgeDays:   30,
		SessionSecret:    "dev-secret-do-not-use-in-production-f8e3a9c2d1b7e6a4",
		SessionTimeout:   604800,
		DataDirectory:    "storage",
		DatabaseFilename: "pinapp.db",
		DatabasePath:     filepath.Join("storage", "pinapp.test.db"),
		envPrefix:        "PINAPP",
	}
	if *cfg != want {
		t.Errorf("got  %+v\nwant %+v", *cfg, want)
	}
	for _, dir := range []string{"storage", "storage/logs"} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("directory %s: %v", dir, err)
		}
	}
}

func TestLoadEnvVars(t *testing.T) {
	t.Run("the prefixed env vars override the defaults", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "development")
		t.Setenv("PINAPP_PORT", "3000")
		t.Setenv("PINAPP_SESSION_SECRET", "from-env")
		t.Setenv("PINAPP_LOG_LEVEL", "warn")
		t.Setenv("PINAPP_DATA_DIR", "data")
		t.Setenv("PINAPP_DEBUG", "true")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Environment != Development || cfg.Port != "3000" || cfg.SessionSecret != "from-env" ||
			cfg.LogLevel != "warn" || cfg.DataDirectory != "data" || !cfg.Debug {
			t.Errorf("env vars not applied: %+v", *cfg)
		}
		if cfg.DatabasePath != filepath.Join("data", "pinapp.development.db") {
			t.Errorf("DatabasePath = %q", cfg.DatabasePath)
		}
	})

	t.Run("an empty env var counts as unset", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		t.Setenv("PINAPP_PORT", "")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Port != "8080" {
			t.Errorf("Port = %q, want the default", cfg.Port)
		}
	})

	t.Run("debug reads 1 as true", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		t.Setenv("PINAPP_DEBUG", "1")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.Debug {
			t.Error("Debug = false, want true")
		}
	})

	t.Run("debug that is not a bool is an error", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		t.Setenv("PINAPP_DEBUG", "yes please")

		_, err := Load("pinapp")

		if err == nil {
			t.Error("Load returned nil, want an error")
		}
	})

	t.Run("an explicit error log level stays error in production", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "production")
		t.Setenv("PINAPP_SESSION_SECRET", "s")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.LogLevel != "error" {
			t.Errorf("LogLevel = %q, want error", cfg.LogLevel)
		}
	})

	t.Run("PRIVATE_KEY is the fallback session secret", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "production")
		t.Setenv("PRIVATE_KEY", "pk")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.SessionSecret != "pk" {
			t.Errorf("SessionSecret = %q, want pk", cfg.SessionSecret)
		}
	})
}

func TestLoadDotEnv(t *testing.T) {
	t.Run("the .env keys are the field keys, in any case", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, `# a comment
PORT=4000

sessionTimeoutSeconds=60
LOGSMAXSIZEINMB=5
LOGSMAXBACKUPS=6
LOGSMAXAGEINDAYS=7
DATABASEMAXOPENCONNS=8
DATABASEMAXIDLECONNS=9
DATABASEFILENAME=custom.sqlite
LOGSDIRECTORY=logs
DEBUG=true
SESSIONSECRET=from-file
`)

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Port != "4000" || cfg.SessionTimeout != 60 || cfg.LogsMaxSizeMB != 5 ||
			cfg.LogsMaxBackups != 6 || cfg.LogsMaxAgeDays != 7 || cfg.MaxOpenConns != 8 ||
			cfg.MaxIdleConns != 9 || cfg.LogsDirectory != "logs" || !cfg.Debug || cfg.SessionSecret != "from-file" {
			t.Errorf(".env not applied: %+v", *cfg)
		}
		if cfg.DatabasePath != filepath.Join("storage", "custom.test.sqlite") {
			t.Errorf("DatabasePath = %q", cfg.DatabasePath)
		}
	})

	t.Run("prefixed keys in .env do nothing", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, "PINAPP_PORT=4000\n")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Port != "8080" {
			t.Errorf("Port = %q, want the default", cfg.Port)
		}
	})

	t.Run("an env var beats the .env file", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		t.Setenv("PINAPP_PORT", "5000")
		writeDotEnv(t, "PORT=4000\nENVIRONMENT=production\n")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Port != "5000" || cfg.Environment != Test {
			t.Errorf("Port = %q, Environment = %q, want 5000 and test", cfg.Port, cfg.Environment)
		}
	})

	t.Run("the .env file can set the environment", func(t *testing.T) {
		inTempDir(t)
		writeDotEnv(t, "ENVIRONMENT=development\n")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.IsDevelopment() {
			t.Errorf("Environment = %q, want development", cfg.Environment)
		}
	})

	t.Run("reads quotes, export, and inline comments", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, "export PORT=\"4000\"\nLOGLEVEL='debug'\nDATADIRECTORY=data # where the db lives\n")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Port != "4000" || cfg.LogLevel != "debug" || cfg.DataDirectory != "data" {
			t.Errorf("Port = %q, LogLevel = %q, DataDirectory = %q", cfg.Port, cfg.LogLevel, cfg.DataDirectory)
		}
	})

	t.Run("an int that is not a number is an error", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, "SESSIONTIMEOUTSECONDS=soon\n")

		_, err := Load("pinapp")

		if err == nil {
			t.Error("Load returned nil, want an error")
		}
	})

	t.Run("does not export .env values to the process", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, "PINAPP_UNUSED_KEY=x\n")

		if _, err := Load("pinapp"); err != nil {
			t.Fatalf("Load: %v", err)
		}

		if _, ok := os.LookupEnv("PINAPP_UNUSED_KEY"); ok {
			t.Error("Load exported a .env value to the environment")
		}
	})

	t.Run("parses values as today", func(t *testing.T) {
		t.Setenv("PINAPP_HOME_PROBE", "/home/probe")
		cases := []struct{ name, content, want string }{
			{"spaces around the equals sign", "PORT = 4000\n", "4000"},
			{"trailing spaces", "PORT=4000 \n", "4000"},
			{"an empty value", "PORT=\n", ""},
			{"a comment without a space", "PORT=40#00\n", "40"},
			{"a hash in double quotes", "PORT=\"40 # 00\"\n", "40 # 00"},
			{"an equals sign in the value", "PORT=a=b\n", "a=b"},
			{"a colon separator", "PORT: 4000\n", "4000"},
			{"a newline escape in double quotes", "PORT=\"a\\nb\"\n", "a\nb"},
			{"an earlier .env key", "A=x\nPORT=${A}y\n", "xy"},
			{"an env var in double quotes", "PORT=\"$PINAPP_HOME_PROBE\"\n", "/home/probe"},
			{"an env var without quotes", "PORT=${PINAPP_HOME_PROBE}\n", "/home/probe"},
			{"no expansion in single quotes", "PORT='$PINAPP_HOME_PROBE'\n", "$PINAPP_HOME_PROBE"},
			{"a malformed line ignores the whole file", "garbage line\nPORT=1\n", "8080"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				inTempDir(t)
				t.Setenv("PINAPP_ENV", "test")
				writeDotEnv(t, tc.content)

				cfg, err := Load("pinapp")

				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if cfg.Port != tc.want {
					t.Errorf("Port = %q, want %q", cfg.Port, tc.want)
				}
			})
		}
	})

	t.Run("APPNAME in .env sets AppName but not the prefix or the file name", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, "APPNAME=other\nDATABASEPATH=/ignored\n")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.AppName != "other" || cfg.DatabasePath != filepath.Join("storage", "pinapp.test.db") {
			t.Errorf("AppName = %q, DatabasePath = %q", cfg.AppName, cfg.DatabasePath)
		}
	})

	t.Run("empty ints and bools are zero", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, "SESSIONTIMEOUTSECONDS=\nDEBUG=\n")

		cfg, err := Load("pinapp")

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.SessionTimeout != 0 || cfg.Debug {
			t.Errorf("SessionTimeout = %d, Debug = %v", cfg.SessionTimeout, cfg.Debug)
		}
	})

	t.Run("a fractional int is an error", func(t *testing.T) {
		inTempDir(t)
		t.Setenv("PINAPP_ENV", "test")
		writeDotEnv(t, "SESSIONTIMEOUTSECONDS=1.5\n")

		_, err := Load("pinapp")

		if err == nil {
			t.Error("Load returned nil, want an error")
		}
	})
}
