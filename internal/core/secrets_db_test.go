package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sqlite"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/store/storetest"
)

// memSecrets is an in-memory credential store.
type memSecrets map[string][]byte

func (m memSecrets) Set(k string, v []byte) error { m[k] = append([]byte(nil), v...); return nil }
func (m memSecrets) Get(k string) ([]byte, error) {
	if v, ok := m[k]; ok {
		return v, nil
	}
	return nil, mygo.ErrSecretNotFound
}
func (m memSecrets) Delete(k string) error { delete(m, k); return nil }

// Spec "Secrets excluded" / "Inspect the database": after profiles with
// passwords and key passphrases are saved through the save path, no TEXT
// or BLOB column, and no byte of the database files, contains them.
func TestSecretsNeverReachTheDatabase(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	creds := memSecrets{}
	sec := secret.New(creds)

	secrets := []string{"Pa55-word-dev-box!", "key-passphrase-7Hq2", "second-pw-Zx9", "proxy-pw-Qm4!"}
	p1 := store.NewProfile()
	p1.Name, p1.Host, p1.Username = "dev-box", "10.0.0.5", "alice"
	r1, err := saveProfile(ctx, st, sec, ProfileInput{Profile: p1,
		Password: SecretInput{Value: secrets[0], Set: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p2 := store.NewProfile()
	p2.Name, p2.Host, p2.Username, p2.AuthMethod = "prod-box", "10.0.0.6", "bob", store.AuthKey
	p2.KeyPath = `C:\Users\alice\.ssh\id_ed25519`
	p2.Proxy = store.Proxy{Type: store.ProxySOCKS5, Host: "proxy.lan", Port: 1080, Username: "bob"}
	r2, err := saveProfile(ctx, st, sec, ProfileInput{Profile: p2,
		Passphrase: SecretInput{Value: secrets[1], Set: true}, ProxyPassword: SecretInput{Value: secrets[3], Set: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Editing keeps going through the same path.
	r1.Profile.RemoteDir = "~/in"
	if _, err := saveProfile(ctx, st, sec, ProfileInput{Profile: r1.Profile,
		Password: SecretInput{Value: secrets[2], Set: true}}, nil); err != nil {
		t.Fatal(err)
	}

	// The secrets are in the credential store.
	if string(creds[secret.Key(r1.Profile.ID, secret.Password)]) != secrets[2] ||
		string(creds[secret.Key(r2.Profile.ID, secret.Passphrase)]) != secrets[1] ||
		string(creds[secret.Key(r2.Profile.ID, secret.ProxyPassword)]) != secrets[3] {
		t.Fatalf("credential store %q", creds)
	}

	// Every TEXT and BLOB value of every table, read with another
	// connection as any SQLite tool would.
	db, err := sqlite.Open(ctx, st.Path(), sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tables, err := db.Query(ctx, "SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		t.Fatal(err)
	}
	cells := 0
	for _, tr := range tables.Values {
		table := tr[0].(string)
		rows, err := db.Query(ctx, `SELECT * FROM "`+table+`"`)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows.Values {
			for i, v := range row {
				var text string
				switch v := v.(type) {
				case string:
					text = v
				case []byte:
					text = string(v)
				default:
					continue
				}
				cells++
				for _, s := range secrets {
					if strings.Contains(text, s) {
						t.Errorf("%s.%s contains a secret", table, rows.Columns[i])
					}
				}
			}
		}
	}
	if cells == 0 {
		t.Fatal("scanned no text cells")
	}

	// And no byte of the files on disk.
	st.Close()
	db.Close()
	matches, _ := filepath.Glob(st.Path() + "*")
	for _, f := range matches {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range secrets {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("%s contains a secret", filepath.Base(f))
			}
		}
	}
}

func TestSaveProfileValidation(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	sec := secret.New(memSecrets{})
	ok := store.NewProfile()
	ok.Name, ok.Host, ok.Username = "dev-box", "10.0.0.5", "alice"
	saved, err := saveProfile(ctx, st, sec, ProfileInput{Profile: ok}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Spec "Create a profile with defaults".
	if saved.Profile.Port != 22 || saved.Profile.RemoteDir != "~/clipboard" {
		t.Errorf("saved %+v", saved.Profile)
	}

	// Spec "Reject invalid profile".
	for _, c := range []struct {
		field string
		edit  func(*store.Profile)
	}{
		{FieldHost, func(p *store.Profile) { p.Host = " " }},
		{FieldPort, func(p *store.Profile) { p.Port = 70000 }},
		{FieldName, func(p *store.Profile) { p.Name = "DEV-BOX" }},
		{FieldUsername, func(p *store.Profile) { p.Username = "" }},
		{FieldKeyPath, func(p *store.Profile) { p.AuthMethod = store.AuthKey }},
		{FieldProxyHost, func(p *store.Profile) { p.Proxy.Type, p.Proxy.Host = store.ProxySOCKS5, " " }},
		{FieldProxyPort, func(p *store.Profile) {
			p.Proxy = store.Proxy{Type: store.ProxySOCKS5, Host: "proxy.lan", Port: 0}
		}},
		{FieldProxyHost, func(p *store.Profile) { p.Proxy.Type, p.Proxy.Host = "http", "proxy.lan" }},
	} {
		p := store.NewProfile()
		p.Name, p.Host, p.Username = "another", "10.0.0.7", "bob"
		c.edit(&p)
		_, err := saveProfile(ctx, st, sec, ProfileInput{Profile: p}, nil)
		fe, isFE := err.(FieldErrors)
		if !isFE || fe[c.field] == nil {
			t.Errorf("%s: got %v", c.field, err)
		}
	}
	list, _ := st.Profiles.List(ctx)
	if len(list) != 1 {
		t.Errorf("invalid profiles were saved: %+v", list)
	}
}
