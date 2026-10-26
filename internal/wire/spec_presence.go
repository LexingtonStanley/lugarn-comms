package wire

import (
	"bytes"
	"encoding/json"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// model's nullable spec fields always marshal as null. Keep omitted nullable
// fields omitted when relaying an app request, without redefining TeamSpec.
func (r *ApplyTeamRequest) UnmarshalJSON(b []byte) error {
	type plain ApplyTeamRequest
	var v plain
	if err := model.DecodeStrict(b, &v); err != nil {
		return err
	}
	var raw struct {
		Spec any `json:"spec"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*r = ApplyTeamRequest(v)
	r.specShape = raw.Spec
	return nil
}
func (r ApplyTeamRequest) MarshalJSON() ([]byte, error) {
	type plain ApplyTeamRequest
	b, err := json.Marshal(plain(r))
	if err != nil || r.specShape == nil {
		return b, err
	}
	var obj map[string]any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err = decoder.Decode(&obj); err != nil {
		return nil, err
	}
	preserveOmittedNulls(obj["spec"], r.specShape)
	return json.Marshal(obj)
}
func preserveOmittedNulls(value, shape any) {
	switch v := value.(type) {
	case map[string]any:
		original, ok := shape.(map[string]any)
		if !ok {
			return
		}
		for k, item := range v {
			before, present := original[k]
			if !present && item == nil {
				delete(v, k)
			} else if present {
				preserveOmittedNulls(item, before)
			}
		}
	case []any:
		original, ok := shape.([]any)
		if !ok {
			return
		}
		for i := 0; i < len(v) && i < len(original); i++ {
			preserveOmittedNulls(v[i], original[i])
		}
	}
}
