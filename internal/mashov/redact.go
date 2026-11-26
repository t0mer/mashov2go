package mashov

import (
	"encoding/json"
	"strings"
)

// redactedValue replaces the value of every PII field (CLAUDE.md §0.5).
const redactedValue = "[redacted]"

// piiKeys are JSON keys whose values are names, ids, phones or credentials.
var piiKeys = map[string]bool{
	"privatename": true, "familyname": true, "displayname": true, "name": true,
	"userid": true, "idnumber": true, "eventuserid": true, "sendername": true,
	"reporter": true, "teachername": true, "groupteachers": true,
	"firstname": true, "lastname": true, "studentname": true, "birthdate": true,
	"address": true, "phonenumber": true, "mobile": true, "eventtext": true,
	"cellphone": true, "phone": true, "email": true, "username": true,
	"password": true, "devicepass": true, "deviceuuid": true, "captcha": true,
}

// isPIIKey reports whether a JSON key holds PII. Any key containing "guid"
// (childGuid, studentGuid, …) is treated as an identifier.
func isPIIKey(k string) bool {
	lk := strings.ToLower(k)
	return piiKeys[lk] || strings.Contains(lk, "guid")
}

// RedactJSON returns b with PII values replaced by "[redacted]". Non-JSON input
// is returned as a placeholder so raw bodies never leak through an error path.
func RedactJSON(b []byte) []byte {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return []byte(`"[non-json body redacted]"`)
	}
	out, err := json.MarshalIndent(redactValue(v), "", "  ")
	if err != nil {
		return []byte(`"[redaction failed]"`)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isPIIKey(k) {
				t[k] = redactedValue
				continue
			}
			t[k] = redactValue(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = redactValue(t[i])
		}
		return t
	default:
		return v
	}
}
