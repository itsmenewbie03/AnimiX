package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestDecodePreferencesNormalizesSupportedTypes(t *testing.T) {
	t.Parallel()
	prefs, err := decodePreferences([]byte(`[
		{"key":"enabled","title":"Enabled","type":"checkBox","value":true},
		{"key":"quality","title":"Quality","type":"list","value":"ass","entries":["WebVTT","ASS"],"entryValues":["vtt","ass"]},
		{"key":"hosts","type":"multi_select","value":["a"],"entries":["Host A","Host B"],"entryValues":["a","b"]},
		{"key":"name","type":"text","value":"value"},
		{"key":"playback","title":"Playback","type":"category"},
		{"key":"login","title":"Login","type":"other","enabled":true}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []PreferenceKind{PreferenceSwitch, PreferenceList, PreferenceMultiSelect, PreferenceText, PreferenceCategory, PreferenceUnsupported}
	for i, want := range wantKinds {
		if prefs[i].Kind != want {
			t.Errorf("preference %d kind = %q, want %q", i, prefs[i].Kind, want)
		}
	}
	if prefs[1].Options[1] != (PreferenceOption{Label: "ASS", Value: "ass"}) {
		t.Fatalf("labels and values were not preserved: %#v", prefs[1].Options)
	}
	if prefs[5].Enabled || prefs[5].Editable() {
		t.Fatal("unsupported action preference must be disabled")
	}
}

func TestDecodePreferencesAcceptsJSONString(t *testing.T) {
	t.Parallel()
	encoded, _ := json.Marshal(`[{"key":"enabled","type":"switch","value":true}]`)
	prefs, err := decodePreferences(encoded)
	if err != nil || len(prefs) != 1 || prefs[0].Value != true {
		t.Fatalf("preferences = %#v, err = %v", prefs, err)
	}
}

type preferenceCaller struct {
	calls []string
	args  []map[string]any
}

func (c *preferenceCaller) Call(_ context.Context, method string, args map[string]any, out any) error {
	c.calls = append(c.calls, method)
	c.args = append(c.args, args)
	if method == "aniyomiSavePreference" {
		*out.(*bool) = true
		return nil
	}
	*out.(*json.RawMessage) = json.RawMessage(`[{"key":"enabled","type":"switch","value":true}]`)
	return nil
}

func TestUpdateSourcePreferenceSavesThenReloads(t *testing.T) {
	t.Parallel()
	caller := &preferenceCaller{}
	service := newService(caller)
	prefs, err := service.UpdateSourcePreference(context.Background(), Source{ID: "42", Type: "anime"}, "enabled", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caller.calls, []string{"aniyomiSavePreference", "aniyomiGetPreferences"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if caller.args[0]["sourceId"] != "42" || caller.args[0]["value"] != true || len(prefs) != 1 {
		t.Fatalf("args = %#v, preferences = %#v", caller.args[0], prefs)
	}
}
