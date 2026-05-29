package detections

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type SigmaImportOptions struct {
	FieldMap             map[string]string
	AllowMissingMetadata bool
}

type sigmaRule struct {
	Title       string         `yaml:"title"`
	ID          string         `yaml:"id"`
	Status      string         `yaml:"status"`
	Description string         `yaml:"description"`
	Tags        []string       `yaml:"tags"`
	Level       string         `yaml:"level"`
	LogSource   sigmaLogSource `yaml:"logsource"`
	Detection   map[string]any `yaml:"detection"`
}

type sigmaLogSource struct {
	Product  string `yaml:"product"`
	Service  string `yaml:"service"`
	Category string `yaml:"category"`
}

func ImportSigma(data []byte, opts SigmaImportOptions) (Rule, error) {
	var raw sigmaRule
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Rule{}, err
	}
	conditionRaw, ok := raw.Detection["condition"]
	if !ok {
		return Rule{}, fmt.Errorf("sigma detection condition is required")
	}
	selections := map[string]Expression{}
	for name, value := range raw.Detection {
		if strings.EqualFold(strings.TrimSpace(name), "condition") {
			continue
		}
		expr, err := sigmaSelectionExpression(value, opts)
		if err != nil {
			return Rule{}, fmt.Errorf("sigma selection %s: %w", name, err)
		}
		selections[strings.TrimSpace(name)] = expr
	}
	expr, err := parseSigmaCondition(fmt.Sprint(conditionRaw), selections)
	if err != nil {
		return Rule{}, err
	}
	rule := Rule{
		ID:          strings.TrimSpace(raw.ID),
		Title:       strings.TrimSpace(raw.Title),
		Description: strings.TrimSpace(raw.Description),
		Status:      strings.TrimSpace(raw.Status),
		Severity:    strings.TrimSpace(raw.Level),
		Tags:        append([]string(nil), raw.Tags...),
		LogSource: LogSource{
			Product:  strings.TrimSpace(raw.LogSource.Product),
			Service:  strings.TrimSpace(raw.LogSource.Service),
			Category: strings.TrimSpace(raw.LogSource.Category),
		},
		Expression: expr,
	}
	if opts.AllowMissingMetadata {
		if !rule.Expression.valid() {
			return Rule{}, fmt.Errorf("rule expression is required")
		}
	} else if err := rule.Validate(); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func sigmaSelectionExpression(raw any, opts SigmaImportOptions) (Expression, error) {
	switch typed := raw.(type) {
	case []any:
		var keywords []Expression
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				keywords = append(keywords, Field("__raw__", OpContains, value))
			}
		}
		if len(keywords) == 0 {
			return Expression{}, fmt.Errorf("keyword selection is empty")
		}
		return Any(keywords...), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return Expression{}, fmt.Errorf("keyword selection is empty")
		}
		return Field("__raw__", OpContains, typed), nil
	case map[string]any:
		parts := make([]Expression, 0, len(typed))
		for rawField, value := range typed {
			field, op := sigmaFieldOperator(rawField)
			if mapped := strings.TrimSpace(opts.FieldMap[field]); mapped != "" {
				field = mapped
			}
			values := anyList(value)
			if op == OpExists {
				parts = append(parts, Field(field, op))
				continue
			}
			if len(values) == 0 {
				return Expression{}, fmt.Errorf("field %s has no values", rawField)
			}
			if len(values) == 1 {
				parts = append(parts, Field(field, op, values[0]))
				continue
			}
			choices := make([]Expression, 0, len(values))
			for _, item := range values {
				choices = append(choices, Field(field, op, item))
			}
			parts = append(parts, Any(choices...))
		}
		if len(parts) == 0 {
			return Expression{}, fmt.Errorf("field selection is empty")
		}
		return All(parts...), nil
	default:
		return Expression{}, fmt.Errorf("unsupported selection type %T", raw)
	}
}

func sigmaFieldOperator(raw string) (string, string) {
	parts := strings.Split(raw, "|")
	field := strings.TrimSpace(parts[0])
	op := OpEquals
	for _, modifier := range parts[1:] {
		switch strings.ToLower(strings.TrimSpace(modifier)) {
		case "contains":
			op = OpContains
		case "startswith":
			op = OpStartsWith
		case "endswith":
			op = OpEndsWith
		case "exists":
			op = OpExists
		}
	}
	return field, op
}

func anyList(value any) []any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		return append([]any(nil), typed...)
	default:
		return []any{typed}
	}
}

type sigmaConditionParser struct {
	tokens     []string
	pos        int
	selections map[string]Expression
}

func parseSigmaCondition(condition string, selections map[string]Expression) (Expression, error) {
	p := sigmaConditionParser{tokens: tokenizeSigmaCondition(condition), selections: selections}
	if len(p.tokens) == 0 {
		return Expression{}, fmt.Errorf("sigma condition is empty")
	}
	expr, err := p.parseOr()
	if err != nil {
		return Expression{}, err
	}
	if p.pos != len(p.tokens) {
		return Expression{}, fmt.Errorf("unexpected sigma condition token %q", p.tokens[p.pos])
	}
	return expr, nil
}

func (p *sigmaConditionParser) parseOr() (Expression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return Expression{}, err
	}
	parts := []Expression{left}
	for p.match("or") {
		right, err := p.parseAnd()
		if err != nil {
			return Expression{}, err
		}
		parts = append(parts, right)
	}
	if len(parts) == 1 {
		return left, nil
	}
	return Any(parts...), nil
}

func (p *sigmaConditionParser) parseAnd() (Expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return Expression{}, err
	}
	parts := []Expression{left}
	for p.match("and") {
		right, err := p.parseUnary()
		if err != nil {
			return Expression{}, err
		}
		parts = append(parts, right)
	}
	if len(parts) == 1 {
		return left, nil
	}
	return All(parts...), nil
}

func (p *sigmaConditionParser) parseUnary() (Expression, error) {
	if p.match("not") {
		child, err := p.parseUnary()
		if err != nil {
			return Expression{}, err
		}
		return Not(child), nil
	}
	if p.match("(") {
		child, err := p.parseOr()
		if err != nil {
			return Expression{}, err
		}
		if !p.match(")") {
			return Expression{}, fmt.Errorf("sigma condition missing closing parenthesis")
		}
		return child, nil
	}
	if p.pos >= len(p.tokens) {
		return Expression{}, fmt.Errorf("unexpected end of sigma condition")
	}
	name := p.tokens[p.pos]
	p.pos++
	expr, ok := p.selections[name]
	if !ok {
		return Expression{}, fmt.Errorf("sigma condition references unknown selection %q", name)
	}
	return expr, nil
}

func (p *sigmaConditionParser) match(token string) bool {
	if p.pos >= len(p.tokens) || !strings.EqualFold(p.tokens[p.pos], token) {
		return false
	}
	p.pos++
	return true
}

func tokenizeSigmaCondition(condition string) []string {
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	for _, r := range condition {
		switch r {
		case ' ', '\t', '\n', '\r':
			flush()
		case '(', ')':
			flush()
			tokens = append(tokens, string(r))
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return tokens
}
