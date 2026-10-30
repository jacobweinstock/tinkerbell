package agent

import (
	"encoding/json"
	"testing"
)

func TestRegistryMarshalJSON(t *testing.T) {
	tests := map[string]struct {
		reg  Registry
		want string
	}{
		"password redacted": {
			reg:  Registry{Name: "reg.example.com", User: "bob", Pass: "s3cret"},
			want: `{"Name":"reg.example.com","User":"bob","Pass":"redacted"}`,
		},
		"empty password stays empty": {
			reg:  Registry{Name: "reg.example.com"},
			want: `{"Name":"reg.example.com","User":"","Pass":""}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Registry is nested, as it is when the Agent configuration is logged.
			o := &Options{Registry: tt.reg}
			b, err := json.Marshal(o)
			if err != nil {
				t.Fatal(err)
			}
			var got struct{ Registry json.RawMessage }
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if string(got.Registry) != tt.want {
				t.Errorf("Registry = %s, want %s", got.Registry, tt.want)
			}
			if o.Registry.Pass != tt.reg.Pass {
				t.Errorf("Pass changed to %q, want %q", o.Registry.Pass, tt.reg.Pass)
			}
		})
	}
}
