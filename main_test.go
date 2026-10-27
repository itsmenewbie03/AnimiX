package main

import (
	"reflect"
	"testing"
)

func TestNormalizeInstallArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "bare", input: []string{"--install-runtime"}, want: []string{"--install-runtime"}},
		{
			name:  "space separated value",
			input: []string{"--install-runtime", "fork@latest", "--diagnose"},
			want:  []string{"--install-runtime=fork@latest", "--diagnose"},
		},
		{
			name:  "equals value",
			input: []string{"--install-runtime=upstream@v2.7.3"},
			want:  []string{"--install-runtime=upstream@v2.7.3"},
		},
		{
			name:  "next flag is not consumed",
			input: []string{"--install-runtime", "--diagnose"},
			want:  []string{"--install-runtime", "--diagnose"},
		},
		{
			name:  "disabled form",
			input: []string{"--install-runtime=false"},
			want:  []string{"--install-runtime=false"},
		},
		{
			name:  "other flags preserved",
			input: []string{"--runtime", "/tmp/runtime.jar", "--install-runtime", "fork"},
			want:  []string{"--runtime", "/tmp/runtime.jar", "--install-runtime=fork"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeInstallArgs(test.input); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("normalizeInstallArgs(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestInstallRuntimeFlag(t *testing.T) {
	t.Parallel()
	var install installRuntimeFlag
	if err := install.Set("true"); err != nil {
		t.Fatal(err)
	}
	if !install.enabled || install.spec != "" {
		t.Fatalf("bare install = %#v", install)
	}
	if err := install.Set("fork@latest"); err != nil {
		t.Fatal(err)
	}
	if !install.enabled || install.spec != "fork@latest" {
		t.Fatalf("spec install = %#v", install)
	}
	if err := install.Set("false"); err != nil {
		t.Fatal(err)
	}
	if install.enabled {
		t.Fatalf("disabled install = %#v", install)
	}
}
