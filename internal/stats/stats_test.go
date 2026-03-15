package stats

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("could not connect to docker: %v", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "17",
		Env: []string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"POSTGRES_DB=test",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatalf("could not start postgres: %v", err)
	}

	dsn := fmt.Sprintf("host=localhost port=%s user=test password=test dbname=test sslmode=disable",
		resource.GetPort("5432/tcp"))

	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		return db.Ping()
	}); err != nil {
		log.Fatalf("could not connect to postgres: %v", err)
	}

	testDB, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	if err := Migrate(testDB); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	code := m.Run()

	if err := pool.Purge(resource); err != nil {
		log.Printf("could not purge resource: %v", err)
	}

	os.Exit(code)
}

func truncMin(t time.Time) time.Time {
	return t.UTC().Truncate(time.Minute)
}

func queryCount(t *testing.T, username string, ts time.Time) int {
	t.Helper()
	var count int
	err := testDB.QueryRow(
		`SELECT submission_count FROM submissions WHERE username=$1 AND timestamp=$2`,
		username, truncMin(ts),
	).Scan(&count)
	require.NoError(t, err)
	return count
}

func cleanup(t *testing.T) {
	t.Helper()
	_, err := testDB.Exec("DELETE FROM submissions")
	require.NoError(t, err)
}

func TestUpsertSubmission_FirstEntry(t *testing.T) {
	cleanup(t)
	now := time.Now()
	require.NoError(t, UpsertSubmission(testDB, "alice", now))
	assert.Equal(t, 1, queryCount(t, "alice", now))
}

func TestUpsertSubmission_SameMinute(t *testing.T) {
	cleanup(t)
	// Truncate to minute boundary so +30s stays within the same minute.
	now := time.Now().Truncate(time.Minute)
	require.NoError(t, UpsertSubmission(testDB, "alice", now))
	require.NoError(t, UpsertSubmission(testDB, "alice", now.Add(30*time.Second)))
	assert.Equal(t, 2, queryCount(t, "alice", now))
}

func TestUpsertSubmission_NewMinute(t *testing.T) {
	cleanup(t)
	t1 := time.Now()
	t2 := t1.Add(2 * time.Minute)
	require.NoError(t, UpsertSubmission(testDB, "alice", t1))
	require.NoError(t, UpsertSubmission(testDB, "alice", t2))
	assert.Equal(t, 1, queryCount(t, "alice", t1))
	assert.Equal(t, 1, queryCount(t, "alice", t2))
}

func TestUpsertSubmission_MultipleUsers(t *testing.T) {
	cleanup(t)
	now := time.Now()
	require.NoError(t, UpsertSubmission(testDB, "alice", now))
	require.NoError(t, UpsertSubmission(testDB, "bob", now))
	assert.Equal(t, 1, queryCount(t, "alice", now))
	assert.Equal(t, 1, queryCount(t, "bob", now))
}
