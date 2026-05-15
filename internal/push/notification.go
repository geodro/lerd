package push

import "encoding/json"

// Notification is the kind-agnostic shape every push or WS broadcast carries.
// Title/Body are pre-resolved English fallbacks; TitleKey/BodyKey + Params
// drive Paraglide localisation on the page side. TTL/Urgency are HTTP-only.
type Notification struct {
	Kind     string            `json:"kind"`
	Title    string            `json:"title,omitempty"`
	TitleKey string            `json:"title_key,omitempty"`
	Body     string            `json:"body,omitempty"`
	BodyKey  string            `json:"body_key,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Tag      string            `json:"tag,omitempty"`
	URL      string            `json:"url,omitempty"`
	Icon     string            `json:"icon,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
	TTL      int               `json:"-"`
	Urgency  string            `json:"-"`
}

func (n Notification) Payload() ([]byte, error) {
	return json.Marshal(n)
}
