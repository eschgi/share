package db

import "testing"

func TestRebind(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"SELECT 1", "SELECT 1"},
		{"SELECT * FROM files WHERE id = ? AND state = ?", "SELECT * FROM files WHERE id = $1 AND state = $2"},
		{"UPDATE files SET a = ?1, b = ?2 WHERE c = ?1 OR d = ?2", "UPDATE files SET a = $1, b = $2 WHERE c = $1 OR d = $2"},
		{"WHERE x = ?1 AND y = ?", "WHERE x = $1 AND y = $2"}, // one more than the highest so far
		{`name LIKE ? ESCAPE '\'`, `name LIKE $1 ESCAPE '\'`},
		{"SELECT '?', 'it''s ?', \"col?\" FROM t WHERE a = ?", "SELECT '?', 'it''s ?', \"col?\" FROM t WHERE a = $1"},
		{"SELECT ? -- what?\nFROM t /* or ? */ WHERE b = ?", "SELECT $1 -- what?\nFROM t /* or ? */ WHERE b = $2"},
		{"INSERT INTO meta (key, value) VALUES ('server_id', ?) ON CONFLICT DO NOTHING", "INSERT INTO meta (key, value) VALUES ('server_id', $1) ON CONFLICT DO NOTHING"},
		{"SELECT 'unterminated ?", "SELECT 'unterminated ?"},
	} {
		if got := rebind(tc.in); got != tc.want {
			t.Errorf("rebind(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}
