package schema

type Component interface {
	GetType() string
}

func (b BaseComponent) GetType() string {
	return b.Type
}

type BaseComponent struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type TextComponent struct {
	BaseComponent
	MinLength *int `json:"minLength"`
	MaxLength *int `json:"maxLength"`
}

type IntegerComponent struct {
	BaseComponent
	Min *int `json:"min"`
	Max *int `json:"max"`
}

type ReferenceComponent struct {
	BaseComponent
	EntityType string `json:"entityType"`
}

type DatetimeComponent struct {
	BaseComponent
	Min *string `json:"min"`
	Max *string `json:"max"`
}

type UrlComponent struct {
	BaseComponent
}

type EmailComponent struct {
	BaseComponent
}

type BoolComponent struct {
	BaseComponent
}

var ComponentMap = map[string]func() Component{
	"text":      func() Component { return &TextComponent{} },
	"integer":   func() Component { return &IntegerComponent{} },
	"reference": func() Component { return &ReferenceComponent{} },
	"datetime":  func() Component { return &DatetimeComponent{} },
	"url":       func() Component { return &UrlComponent{} },
	"email":     func() Component { return &EmailComponent{} },
	"boolean":   func() Component { return &BoolComponent{} },
}
