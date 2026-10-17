package dbquery

import "testing"

func TestIsReadOnlySQL(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"  select t, tip AS value FROM livecomments",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"-- 得点\nSELECT 1",
		"/* note */ SELECT 1",
		"# mysql comment\n  select 1",
	}
	ng := []string{
		"UPDATE livecomments SET tip = 0",
		"DELETE FROM users",
		"DROP TABLE users",
		"TRUNCATE livecomments",
		"INSERT INTO users VALUES (1)",
		"-- SELECT\nDELETE FROM users",
		"/* SELECT */ DROP TABLE users",
		"/* unterminated SELECT 1",
		"",
	}
	for _, q := range ok {
		if !isReadOnlySQL(q) {
			t.Errorf("should be allowed: %q", q)
		}
	}
	for _, q := range ng {
		if isReadOnlySQL(q) {
			t.Errorf("should be rejected: %q", q)
		}
	}
}

func TestParseConfigRejectsWrites(t *testing.T) {
	_, err := ParseConfig([]byte("dsn: x\nqueries:\n  - {name: bad, sql: 'DELETE FROM users'}\n"))
	if err == nil {
		t.Fatal("a write query must be rejected when the config is saved")
	}
	cfg, err := ParseConfig([]byte("dsn: x\ntotal_timeout: 10s\nqueries:\n  - {name: ok, sql: 'SELECT 1'}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.totalTimeout().Seconds() != 10 || cfg.timeout().Seconds() != 30 {
		t.Errorf("timeouts = %v / %v", cfg.totalTimeout(), cfg.timeout())
	}
}
