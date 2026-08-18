package nodes

import (
	"regexp"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// Supported condition operators. They are named here rather than inline so the
// unknown-operator error and any future descriptor-driven operator list stay in
// step with what matchCondition actually implements.
const (
	OpEquals      = "equals"
	OpNotEquals   = "notEquals"
	OpContains    = "contains"
	OpNotContains = "notContains"
	OpStartsWith  = "startsWith"
	OpEndsWith    = "endsWith"
	OpGreater     = "gt"
	OpGreaterEq   = "gte"
	OpLess        = "lt"
	OpLessEq      = "lte"
	OpIsEmpty     = "isEmpty"
	OpIsNotEmpty  = "isNotEmpty"
	OpIsTrue      = "isTrue"
	OpIsFalse     = "isFalse"
	OpRegex       = "regex"
)

// If routes each item to one of two outputs. It runs perItem on purpose: that is
// what makes the engine re-resolve the conditions against the item being tested,
// so `{{ $json.status }}` means this item's status and every item is routed on
// its own merits. Evaluating once for the whole set would silently send all
// items the way item 0 happened to go.
type If struct{}

// Descriptor implements Node.
func (If) Descriptor() Descriptor {
	return Descriptor{
		Type:        "if",
		Name:        "IF",
		Category:    CategoryFlow,
		Description: "Sends each item down the true or the false branch.",
		Icon:        "branch",
		Mode:        ModePerItem,
		Inputs:      MainIn,
		Outputs: []Handle{
			{Name: HandleTrue, Label: "True"},
			{Name: HandleFalse, Label: "False"},
		},
		Params: []ParamSpec{
			{
				Name:               "conditions",
				Label:              "Conditions",
				Type:               ParamFilter,
				Required:           true,
				Description:        "Each row compares a value against another. Both sides accept expressions.",
				SupportsExpression: true,
			},
			{
				Name:    "combinator",
				Label:   "Combine",
				Type:    ParamSelect,
				Default: "all",
				Options: []ParamOption{
					{Label: "All conditions must match (AND)", Value: "all"},
					{Label: "Any condition may match (OR)", Value: "any"},
				},
			},
		},
	}
}

// Execute tests the current item and puts it on exactly one output handle. Both
// handles are always present in the result, the unused one empty, so the engine
// can tell "no items took this branch" from "this branch does not exist".
func (If) Execute(ec ExecContext) (Result, error) {
	conditions, err := ec.Params.Conditions("conditions")
	if err != nil {
		return Result{}, err
	}
	if len(conditions) == 0 {
		return Result{}, ErrRequiredParam("conditions")
	}
	matched, err := evaluateConditions(conditions, ec.Params.StringOr("combinator", "all"))
	if err != nil {
		return Result{}, err
	}

	taken, other := HandleTrue, HandleFalse
	if !matched {
		taken, other = HandleFalse, HandleTrue
	}
	return Result{Outputs: map[string][]domain.Item{
		taken: {ec.Item},
		other: {},
	}}, nil
}

// evaluateConditions combines the rows with AND or OR.
func evaluateConditions(conditions []Condition, combinator string) (bool, error) {
	switch combinator {
	case "", "all":
		for _, condition := range conditions {
			ok, err := matchCondition(condition)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case "any":
		for _, condition := range conditions {
			ok, err := matchCondition(condition)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, domain.Errorf(domain.ErrCodeValidation, "unknown combinator %q", combinator)
	}
}

// matchCondition applies one operator. An unknown operator is a validation error
// rather than a false result: a typo in a saved graph must be visible, not
// quietly route every item down the false branch.
func matchCondition(c Condition) (bool, error) {
	switch c.Operator {
	case OpEquals:
		return looseEqual(c.Left, c.Right), nil
	case OpNotEquals:
		return !looseEqual(c.Left, c.Right), nil
	case OpContains:
		return strings.Contains(stringify(c.Left), stringify(c.Right)), nil
	case OpNotContains:
		return !strings.Contains(stringify(c.Left), stringify(c.Right)), nil
	case OpStartsWith:
		return strings.HasPrefix(stringify(c.Left), stringify(c.Right)), nil
	case OpEndsWith:
		return strings.HasSuffix(stringify(c.Left), stringify(c.Right)), nil
	case OpGreater:
		return compareValues(c.Left, c.Right) > 0, nil
	case OpGreaterEq:
		return compareValues(c.Left, c.Right) >= 0, nil
	case OpLess:
		return compareValues(c.Left, c.Right) < 0, nil
	case OpLessEq:
		return compareValues(c.Left, c.Right) <= 0, nil
	case OpIsEmpty:
		return isEmptyValue(c.Left), nil
	case OpIsNotEmpty:
		return !isEmptyValue(c.Left), nil
	case OpIsTrue:
		return truthy(c.Left), nil
	case OpIsFalse:
		return !truthy(c.Left), nil
	case OpRegex:
		return matchRegex(c.Left, c.Right)
	default:
		return false, domain.Errorf(domain.ErrCodeValidation, "unknown operator %q", c.Operator)
	}
}

// matchRegex reports whether the left value matches the pattern on the right. An
// invalid pattern is a validation error, since it can only be a mistake in the
// workflow rather than in the data.
func matchRegex(left, right any) (bool, error) {
	pattern := stringify(right)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, domain.Errorf(domain.ErrCodeValidation, "invalid regular expression %q: %v", pattern, err)
	}
	return re.MatchString(stringify(left)), nil
}
