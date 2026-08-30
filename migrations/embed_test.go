package migrations

import "testing"

func TestMigrationVersion(t *testing.T) {
	for _, test := range []struct {
		name    string
		want    int64
		wantErr bool
	}{
		{name: "0001_initial.sql", want: 1},
		{name: "12_add_index.sql", want: 12},
		{name: "initial.sql", wantErr: true},
		{name: "0000_initial.sql", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := migrationVersion(test.name)
			if test.wantErr {
				if err == nil {
					t.Fatalf("migrationVersion(%q) succeeded, want error", test.name)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("migrationVersion(%q) = %d, %v; want %d", test.name, got, err, test.want)
			}
		})
	}
}

func TestUpSQL(t *testing.T) {
	sql, err := upSQL("-- +goose Up\nCREATE TABLE sample (id INT);\n-- +goose Down\nDROP TABLE sample;")
	if err != nil {
		t.Fatal(err)
	}
	if sql != "CREATE TABLE sample (id INT);" {
		t.Fatalf("upSQL() = %q", sql)
	}
	if _, err := upSQL("CREATE TABLE sample (id INT);"); err == nil {
		t.Fatal("upSQL() accepted migration without Up marker")
	}
}
