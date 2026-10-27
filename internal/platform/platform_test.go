package platform

import (
	"strings"
	"testing"
)

func TestXDG(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Dirs
	}{
		{
			name: "defaults",
			env:  map[string]string{"HOME": "/home/a"},
			want: Dirs{Config: "/home/a/.config", Data: "/home/a/.local/share", State: "/home/a/.local/state", Cache: "/home/a/.cache"},
		},
		{
			name: "overrides",
			env: map[string]string{
				"HOME": "/home/a", "XDG_CONFIG_HOME": "/c", "XDG_DATA_HOME": "/d",
				"XDG_STATE_HOME": "/s", "XDG_CACHE_HOME": "/k", "XDG_RUNTIME_DIR": "/run/user/1000",
			},
			want: Dirs{Config: "/c", Data: "/d", State: "/s", Cache: "/k", Runtime: "/run/user/1000"},
		},
		{
			name: "relative values are ignored",
			env:  map[string]string{"HOME": "/home/a", "XDG_CONFIG_HOME": "rel", "XDG_RUNTIME_DIR": "rel"},
			want: Dirs{Config: "/home/a/.config", Data: "/home/a/.local/share", State: "/home/a/.local/state", Cache: "/home/a/.cache"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := XDG(func(k string) string { return tt.env[k] })
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
	if _, err := XDG(func(string) string { return "" }); err == nil {
		t.Error("want an error without HOME")
	}
}

func TestParseOSRelease(t *testing.T) {
	src := "# comment\nID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\nPRETTY_NAME='Ubuntu 24.04'\n"
	got, err := ParseOSRelease(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ID": "ubuntu", "VERSION_ID": "24.04", "VERSION_CODENAME": "noble", "PRETTY_NAME": "Ubuntu 24.04"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
