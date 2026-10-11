package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Every setting of config.json can also come from an environment variable, for platforms where
// a file is awkward, such as Cloud Run: SHARE_ and the setting's path in capitals, with _ between
// the levels, as in SHARE_PUBLIC_URL, SHARE_DATABASE_POSTGRES or SHARE_S3_BUCKET. A variable
// wins over config.json, which wins over the defaults.
const envPrefix = "SHARE_"

// envIgnored are SHARE_ variables that aren't settings: the commit scripts/version.sh names, and
// the tests' database.
var envIgnored = []string{"SHARE_COMMIT", "SHARE_TEST_POSTGRES"}

// envSetting is a setting a variable can set.
type envSetting struct {
	name string       // SHARE_S3_ACCESS_KEY_ID
	path []string     // s3, access_key_id
	kind reflect.Kind // String, Int, Bool, Slice; Struct for a value given as JSON or a word, such as "proxy"
}

var envSettings = settingsOf(reflect.TypeFor[Config](), nil)

// settingsOf lists the settings of a part of config.json. An object becomes one variable per
// field, unless it decodes itself, as "proxy" and "https.certificate" do: then it takes the
// same JSON, or the word, that config.json takes.
func settingsOf(t reflect.Type, path []string) []envSetting {
	unmarshaler := reflect.TypeFor[json.Unmarshaler]()
	var out []envSetting
	for f := range t.Fields() {
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "" || key == "-" {
			continue
		}
		p := append(slices.Clone(path), key)
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && !reflect.PointerTo(ft).Implements(unmarshaler) {
			out = append(out, settingsOf(ft, p)...)
			continue
		}
		kind := ft.Kind()
		if kind >= reflect.Int && kind <= reflect.Int64 {
			kind = reflect.Int
		}
		out = append(out, envSetting{name: envPrefix + strings.ToUpper(strings.Join(p, "_")), path: p, kind: kind})
	}
	return out
}

// value turns a variable into the JSON of its setting.
func (s envSetting) value(v string) (any, error) {
	switch s.kind {
	case reflect.String:
		return v, nil
	case reflect.Int:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil, errors.New("must be a whole number")
		}
		return n, nil
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return nil, errors.New("must be true or false")
		}
		return b, nil
	case reflect.Slice:
		// A JSON list, or words between commas: en,de,it.
		if strings.HasPrefix(strings.TrimSpace(v), "[") {
			return json.RawMessage(v), nil
		}
		list := []string{}
		for w := range strings.SplitSeq(v, ",") {
			if w = strings.TrimSpace(w); w != "" {
				list = append(list, w)
			}
		}
		return list, nil
	default:
		// JSON, as config.json has it, or a word: x-forwarded, self-signed.
		if json.Valid([]byte(v)) {
			return json.RawMessage(v), nil
		}
		return strings.TrimSpace(v), nil
	}
}

// fromEnv lays the settings in environ (KEY=value, as os.Environ gives them) over config.json's
// data. PORT, which platforms such as Cloud Run set, is the http port when neither
// config.json nor SHARE_HTTP_LISTEN names one. used reports whether a SHARE_ variable set
// anything.
func fromEnv(data []byte, environ []string) (out []byte, used bool, err error) {
	vars := map[string]string{}
	var problems []string
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		if v == "" || !strings.HasPrefix(k, envPrefix) && k != "PORT" || slices.Contains(envIgnored, k) {
			continue
		}
		vars[k] = v
		if k != "PORT" && !slices.ContainsFunc(envSettings, func(s envSetting) bool { return s.name == k }) {
			problems = append(problems, k+": is not a setting; the settings are config.json's, e.g. SHARE_PUBLIC_URL or SHARE_S3_BUCKET")
		}
	}
	if len(vars) == 0 {
		return data, false, nil
	}

	top := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&top); err != nil {
		return data, false, nil // the decoder reports what is wrong with it again
	}
	for _, s := range envSettings {
		v, ok := vars[s.name]
		if !ok {
			continue
		}
		val, err := s.value(v)
		if err != nil {
			problems = append(problems, s.name+": "+err.Error())
			continue
		}
		set(top, s.path, val)
		used = true
	}
	if port, ok := vars["PORT"]; ok {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			problems = append(problems, "PORT: must be a port number, e.g. 8080")
		} else if http, ok := top["http"].(map[string]any); ok {
			if _, named := http["listen"]; !named {
				http["listen"] = ":" + port
			}
		} else if _, null := top["http"]; !null {
			top["http"] = map[string]any{"listen": ":" + port}
		}
	}
	if len(problems) > 0 {
		return nil, used, errors.New("environment:\n  " + strings.Join(problems, "\n  "))
	}
	out, err = json.Marshal(top)
	if err != nil {
		return nil, used, fmt.Errorf("environment: %w", err)
	}
	return out, used, nil
}

// set puts val at path, making the objects on the way; one that was something else, such as
// "http": null, becomes an object.
func set(m map[string]any, path []string, val any) {
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[path[len(path)-1]] = val
}

// envUsed reports whether a SHARE_ variable in environ sets a setting.
func envUsed(environ []string) bool {
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		if v != "" && slices.ContainsFunc(envSettings, func(s envSetting) bool { return s.name == k }) {
			return true
		}
	}
	return false
}
