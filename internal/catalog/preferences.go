package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PreferenceKind is the normalized kind of a source-owned preference.
type PreferenceKind string

const (
	PreferenceSwitch      PreferenceKind = "switch"
	PreferenceList        PreferenceKind = "list"
	PreferenceMultiSelect PreferenceKind = "multi_select"
	PreferenceText        PreferenceKind = "text"
	PreferenceCategory    PreferenceKind = "category"
	PreferenceUnsupported PreferenceKind = "unsupported"
)

type PreferenceOption struct {
	Label string
	Value string
}

// Preference is independent of the Android preference classes exposed by the
// runtime. Value is bool, string, []string, or nil according to Kind.
type Preference struct {
	Key     string
	Title   string
	Summary string
	Kind    PreferenceKind
	Enabled bool
	Value   any
	Options []PreferenceOption
}

func (p Preference) Editable() bool {
	return p.Enabled && p.Key != "" && p.Kind != PreferenceCategory && p.Kind != PreferenceUnsupported
}

type runtimePreference struct {
	Key         string          `json:"key"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Type        string          `json:"type"`
	Enabled     *bool           `json:"enabled"`
	Value       json.RawMessage `json:"value"`
	Entries     []string        `json:"entries"`
	EntryValues []string        `json:"entryValues"`
}

func decodePreferences(data []byte) ([]Preference, error) {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return nil, err
		}
		data = []byte(encoded)
	}
	var records []runtimePreference
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	preferences := make([]Preference, 0, len(records))
	for index, record := range records {
		preference, err := normalizePreference(record)
		if err != nil {
			return nil, fmt.Errorf("preference %d (%q): %w", index, record.Key, err)
		}
		preferences = append(preferences, preference)
	}
	return preferences, nil
}

func normalizePreference(record runtimePreference) (Preference, error) {
	enabled := true
	if record.Enabled != nil {
		enabled = *record.Enabled
	}
	p := Preference{Key: record.Key, Title: record.Title, Summary: record.Summary, Enabled: enabled}
	switch strings.ToLower(record.Type) {
	case "switch", "checkbox":
		p.Kind = PreferenceSwitch
		var value bool
		if len(record.Value) > 0 && !bytes.Equal(record.Value, []byte("null")) {
			if err := json.Unmarshal(record.Value, &value); err != nil {
				return Preference{}, fmt.Errorf("decode boolean value: %w", err)
			}
		}
		p.Value = value
	case "list":
		p.Kind = PreferenceList
		p.Value = decodeString(record.Value)
		p.Options = preferenceOptions(record.Entries, record.EntryValues)
	case "multi_select", "multiselect":
		p.Kind = PreferenceMultiSelect
		var values []string
		if len(record.Value) > 0 && !bytes.Equal(record.Value, []byte("null")) {
			if err := json.Unmarshal(record.Value, &values); err != nil {
				return Preference{}, fmt.Errorf("decode multi-select value: %w", err)
			}
		}
		p.Value = values
		p.Options = preferenceOptions(record.Entries, record.EntryValues)
	case "text", "edittext":
		p.Kind = PreferenceText
		p.Value = decodeString(record.Value)
	case "category":
		p.Kind = PreferenceCategory
		p.Enabled = false
	default:
		if record.Key == "" {
			p.Kind = PreferenceCategory
			p.Enabled = false
		} else {
			p.Kind = PreferenceUnsupported
			p.Enabled = false
		}
	}
	return p, nil
}

func decodeString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func preferenceOptions(labels, values []string) []PreferenceOption {
	length := len(values)
	if len(labels) > length {
		length = len(labels)
	}
	options := make([]PreferenceOption, 0, length)
	for i := 0; i < length; i++ {
		var label, value string
		if i < len(labels) {
			label = labels[i]
		}
		if i < len(values) {
			value = values[i]
		}
		if value == "" {
			value = label
		}
		if label == "" {
			label = value
		}
		options = append(options, PreferenceOption{Label: label, Value: value})
	}
	return options
}

func (s *Service) SourcePreferences(ctx context.Context, source Source) ([]Preference, error) {
	var payload json.RawMessage
	if err := s.bridge.Call(ctx, "aniyomiGetPreferences", map[string]any{
		"sourceId": source.ID,
		"isAnime":  source.IsAnime(),
	}, &payload); err != nil {
		return nil, err
	}
	preferences, err := decodePreferences(payload)
	if err != nil {
		return nil, fmt.Errorf("decode source preferences: %w", err)
	}
	return preferences, nil
}

// UpdateSourcePreference persists a value through the runtime and reloads the
// whole screen so extension listeners and dependent states are reflected.
func (s *Service) UpdateSourcePreference(ctx context.Context, source Source, key string, value any) ([]Preference, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("preference key is required")
	}
	var saved bool
	if err := s.bridge.Call(ctx, "aniyomiSavePreference", map[string]any{
		"sourceId": source.ID,
		"isAnime":  source.IsAnime(),
		"key":      key,
		"value":    value,
	}, &saved); err != nil {
		return nil, err
	}
	if !saved {
		return nil, fmt.Errorf("runtime rejected preference %q", key)
	}
	return s.SourcePreferences(ctx, source)
}
