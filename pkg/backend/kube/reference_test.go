package kube

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResolveReferences(t *testing.T) {
	hw := &tinkerbell.Hardware{
		ObjectMeta: metav1.ObjectMeta{Name: "machine1", Namespace: "tink-system"},
		Spec: tinkerbell.HardwareSpec{References: map[string]tinkerbell.Reference{
			"cm":     {Name: "cm1", Namespace: "tink-system", Version: "v1", Resource: "configmaps"},
			"secret": {Name: "s1", Namespace: "tink-system", Version: "v1", Resource: "secrets"},
		}},
	}

	tests := map[string]struct {
		allow, deny []string
		consumer    string
		readErr     error
		want        []string
		wantErr     bool
	}{
		"deny all by default": {
			wantErr: true,
		},
		"allow list overrides the default deny": {
			allow: []string{`{"source":{"namespace":["tink-system"]}}`},
			want:  []string{"cm", "secret"},
		},
		"deny list without allow list": {
			deny:    []string{`{"reference":{"resource":["secrets"]}}`},
			want:    []string{"cm"},
			wantErr: true,
		},
		"rules can match the consumer": {
			allow:    []string{`{"consumer":["tink-controller"],"reference":{"resource":["configmaps"]}}`},
			consumer: "tink-controller",
			want:     []string{"cm"},
			wantErr:  true,
		},
		"rules for another consumer do not match": {
			allow:    []string{`{"consumer":["smee"]}`},
			consumer: "tink-controller",
			wantErr:  true,
		},
		"invalid rule": {
			allow:   []string{"not a rule"},
			wantErr: true,
		},
		"read error": {
			allow:   []string{`{"source":{"namespace":["tink-system"]}}`},
			readErr: errors.New("boom"),
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b := &Backend{
				dynamicClient:           &fakeDynamicClient{gvr: schema.GroupVersionResource{Version: "v1"}, error: tt.readErr},
				ReferenceAllowListRules: tt.allow,
				ReferenceDenyListRules:  tt.deny,
			}
			got, err := b.ResolveReferences(context.Background(), tt.consumer, hw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			var names []string
			for k := range got {
				names = append(names, k)
			}
			slices.Sort(names)
			if !slices.Equal(names, tt.want) {
				t.Errorf("resolved %v, want %v", names, tt.want)
			}
		})
	}
}

// TestDocumentedRulesMatchWithConsumer guards the rule examples in
// docs/technical/REFERENCES.md, which predate the consumer field.
func TestDocumentedRulesMatchWithConsumer(t *testing.T) {
	ed := evaluationData{
		Consumer:  "tink-controller",
		Source:    source{Name: "example1", Namespace: "tink-system"},
		Reference: tinkerbell.Reference{Namespace: "example", Name: "exampleLVM", Group: "example.org", Version: "v1alpha1", Resource: "lvms"},
	}
	for _, rule := range []string{
		`{"source":{"namespace":["tink-system"]}}`,
		`{"reference":{"resource":["lvms"]}}`,
		`{"source":{"namespace":["tink-system"]},"reference":{"resource":["lvms"]}}`,
		`{"source":{"name":["example1"],"namespace":["tink-system"]},"reference":{"name":["exampleLVM"],"namespace":["example"],"resource":["lvms"]}}`,
	} {
		matched, _, err := evaluate(context.Background(), []string{rule}, ed)
		if err != nil || !matched {
			t.Errorf("rule %s: matched = %v, err = %v", rule, matched, err)
		}
	}
}

func TestMatch(t *testing.T) {
	tests := map[string]struct {
		rules         []string
		data          evaluationData
		expectedMatch bool
		expectedRules string
		expectedErr   bool
	}{
		"no match empty rules": {
			rules: []string{},
			data: evaluationData{
				Reference: tinkerbell.Reference{
					Namespace: "tink",
					Name:      "example",
					Group:     "tinkerbell.org",
					Version:   "v1alpha1",
					Resource:  "hardware",
				},
			},
			expectedMatch: false,
		},
		"no match empty data struct": {
			rules:         []string{`{"reference":{"name":[{"wildcard":"*"}]}}`},
			data:          evaluationData{Reference: tinkerbell.Reference{}},
			expectedMatch: false,
		},
		"no match": {
			rules: []string{`{"reference":{"resource":["workflows"]}},{"version":["example"]}`},
			data: evaluationData{
				Reference: tinkerbell.Reference{
					Namespace: "tink",
					Name:      "example",
					Group:     "tinkerbell.org",
					Version:   "v1alpha1",
					Resource:  "hardware",
				},
			},
			expectedMatch: false,
		},
		"match": {
			rules: []string{`{"reference":{"name":["example"]}}`},
			data: evaluationData{
				Reference: tinkerbell.Reference{
					Namespace: "tink",
					Name:      "example",
					Group:     "tinkerbell.org",
					Version:   "v1alpha1",
					Resource:  "hardware",
				},
			},
			expectedMatch: true,
			expectedRules: `pattern-{"reference":{"name":["example"]}}`,
		},
		"deny all": {
			rules: []string{`{"reference":{"name":[{"wildcard":"*"}]}}`},
			data: evaluationData{
				Reference: tinkerbell.Reference{
					Namespace: "tink",
					Name:      "example",
					Group:     "tinkerbell.org",
					Version:   "v1alpha1",
					Resource:  "hardware",
				},
			},
			expectedMatch: true,
			expectedRules: `pattern-{"reference":{"name":[{"wildcard":"*"}]}}`,
		},
		"bad rule": {
			rules: []string{"this is not the rule format"},
			data: evaluationData{
				Reference: tinkerbell.Reference{
					Namespace: "tink",
					Name:      "example",
					Group:     "tinkerbell.org",
					Version:   "v1alpha1",
					Resource:  "hardware",
				},
			},
			expectedMatch: false,
			expectedErr:   true,
		},
		"match reference and source": {
			rules: []string{`{"reference":{"resource":["hardware"],"namespace":["tink"]},"source":{"namespace":["tink-system"]}}`},
			data: evaluationData{
				Source: source{
					Namespace: "tink-system",
				},
				Reference: tinkerbell.Reference{
					Namespace: "tink",
					Name:      "example",
					Group:     "tinkerbell.org",
					Version:   "v1alpha1",
					Resource:  "hardware",
				},
			},
			expectedMatch: true,
			expectedRules: `pattern-{"reference":{"resource":["hardware"],"namespace":["tink"]},"source":{"namespace":["tink-system"]}}`,
		},
		"case insensitive no match": {
			rules: []string{`{"reference":{"resource":["hardware"],"namespace":["tink"]},"source":{"namespace":["tink-system"]}}`},
			data: evaluationData{
				Source: source{
					Namespace: "tink-system",
				},
				Reference: tinkerbell.Reference{
					Namespace: "tink",
					Name:      "example",
					Group:     "tinkerbell.org",
					Version:   "v1alpha1",
					Resource:  "Hardware",
				},
			},
			expectedMatch: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, rules, err := evaluate(context.TODO(), test.rules, test.data)
			if err != nil && !test.expectedErr {
				t.Fatalf("match() error = %v", err)
			}
			if got != test.expectedMatch {
				t.Errorf("match() found: got = %v, want %v", got, test.expectedMatch)
			}
			if rules != test.expectedRules {
				t.Errorf("match() rules: got = %v, want %v", rules, test.expectedRules)
			}
		})
	}
}
