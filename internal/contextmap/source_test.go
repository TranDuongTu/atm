package contextmap

import "testing"

func TestParseSource(t *testing.T) {
	tests := []struct {
		in      string
		want    Source
		wantErr bool
	}{
		{in: "git:internal/store", want: Source{Kind: KindGit, Locator: "internal/store"}},
		{in: "file:/etc/hosts", want: Source{Kind: KindFile, Locator: "/etc/hosts"}},
		{in: "url:https://go.dev/doc", want: Source{Kind: KindURL, Locator: "https://go.dev/doc"}},
		{in: "external:jira/ATM-441", want: Source{Kind: KindExternal, Locator: "jira/ATM-441"}},
		{in: "internal/store", wantErr: true},  // no kind prefix
		{in: "svn:trunk", wantErr: true},       // unknown kind
		{in: "git:", wantErr: true},            // empty locator
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseSource(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseSource(%q): want error, got %v", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseSource(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ParseSource(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
		if rt := got.String(); rt != tt.in {
			t.Errorf("round-trip: String() = %q, want %q", rt, tt.in)
		}
	}
}

func TestSourceProvable(t *testing.T) {
	for _, tt := range []struct {
		src  Source
		want bool
	}{
		{Source{Kind: KindGit, Locator: "a"}, true},
		{Source{Kind: KindFile, Locator: "a"}, true},
		{Source{Kind: KindURL, Locator: "a"}, true},
		{Source{Kind: KindExternal, Locator: "a"}, false},
	} {
		if got := tt.src.Provable(); got != tt.want {
			t.Errorf("%v.Provable() = %v, want %v", tt.src, got, tt.want)
		}
	}
}