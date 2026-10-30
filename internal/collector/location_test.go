package collector

import "testing"

func TestStripCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:ghp_secret@github.com/me/app.git": "https://github.com/me/app.git",
		"https://github.com/me/app.git":                 "https://github.com/me/app.git",
		"git@github.com:me/app.git":                     "git@github.com:me/app.git",
		"ssh://git@host:2222/me/app.git":                "ssh://host:2222/me/app.git",
	} {
		if got := stripCredentials(in); got != want {
			t.Errorf("stripCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRuntimeVersionFromPath(t *testing.T) {
	if v := RuntimeVersion("/home/a/.nvm/versions/node/v24.19.0/bin/node"); v != "v24.19.0" {
		t.Errorf("got %q", v)
	}
}
