package lockfile

import "testing"

func TestParse_Full(t *testing.T) {
	lf, err := ParseFile("testdata/valid/full.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if lf.Version != Version {
		t.Errorf("version = %d, want %d", lf.Version, Version)
	}
	if len(lf.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(lf.Sources))
	}
	if lf.Sources[0].URL != "github.com/pedromvgomes/agentic-toolkit" {
		t.Errorf("sources[0].url = %q", lf.Sources[0].URL)
	}
	if lf.Sources[1].SHA == "" {
		t.Errorf("sources[1].sha is empty")
	}
}

func TestParse_EmptySources(t *testing.T) {
	lf, err := ParseFile("testdata/valid/empty-sources.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lf.Sources) != 0 {
		t.Errorf("sources len = %d, want 0", len(lf.Sources))
	}
}

func TestParse_InvalidCases(t *testing.T) {
	cases := []struct {
		name string
		path string
		want ErrorKind
	}{
		{"missing-version", "testdata/invalid/missing-version.yaml", ErrMissingRequired},
		{"unsupported-version", "testdata/invalid/unsupported-version.yaml", ErrUnsupportedVersion},
		{"missing-sha", "testdata/invalid/missing-sha.yaml", ErrMissingRequired},
		{"missing-ref", "testdata/invalid/missing-ref.yaml", ErrMissingRequired},
		{"unknown-field", "testdata/invalid/unknown-field.yaml", ErrUnknownField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFile(tc.path)
			if err == nil {
				t.Fatalf("expected error of kind %s, got nil", tc.want)
			}
			if !IsKind(err, tc.want) {
				t.Errorf("got error kind != %s\n  err: %v", tc.want, err)
			}
		})
	}
}
