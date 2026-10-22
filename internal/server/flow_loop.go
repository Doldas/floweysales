package server

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func applyPureLoopTransform(op, input, operand string) (string, error) {
	switch op {
	case "increment", "decrement":
		number, err := strconv.ParseFloat(strings.TrimSpace(input), 64)
		if err != nil {
			return "", fmt.Errorf("loop requires numeric value: %w", err)
		}
		if op == "increment" {
			number++
		} else {
			number--
		}
		return strconv.FormatFloat(number, 'f', -1, 64), nil
	default:
		return transformFlowText(op, input, operand)
	}
}

// executePureLoop is a bounded data operation; it cannot run arbitrary flow nodes.
func executePureLoop(n flowNode, input string) (string, error) {
	if n.LoopLimit < 1 || n.LoopLimit > 100 {
		return "", fmt.Errorf("loop limit must be 1–100")
	}
	switch n.LoopMode {
	case "for":
		value := input
		for i := 0; i < n.LoopLimit; i++ {
			next, err := applyPureLoopTransform(n.Transform, value, n.Value)
			if err != nil {
				return "", err
			}
			value = next
			if len(value) > 6000 {
				return "", fmt.Errorf("loop result too large")
			}
		}
		return value, nil
	case "foreach":
		var list []any
		if err := json.Unmarshal([]byte(input), &list); err != nil {
			return "", fmt.Errorf("foreach needs JSON array: %w", err)
		}
		if len(list) > n.LoopLimit {
			return "", fmt.Errorf("foreach array exceeds configured maximum %d", n.LoopLimit)
		}
		result := make([]string, 0, len(list))
		for _, item := range list {
			value, ok := item.(string)
			if !ok {
				b, _ := json.Marshal(item)
				value = string(b)
			}
			transformed, err := applyPureLoopTransform(n.Transform, value, n.Value)
			if err != nil {
				return "", err
			}
			result = append(result, transformed)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		if len(encoded) > 6000 {
			return "", fmt.Errorf("loop result too large")
		}
		return string(encoded), nil
	case "while", "do_while":
		value := input
		for i := 0; i < n.LoopLimit; i++ {
			if n.LoopMode == "while" {
				keep, err := flowV2Condition(value, n.Operator, n.LoopCondition)
				if err != nil {
					return "", err
				}
				if !keep {
					return value, nil
				}
			}
			next, err := applyPureLoopTransform(n.Transform, value, n.Value)
			if err != nil {
				return "", err
			}
			value = next
			if len(value) > 6000 {
				return "", fmt.Errorf("loop result too large")
			}
			if n.LoopMode == "do_while" {
				keep, err := flowV2Condition(value, n.Operator, n.LoopCondition)
				if err != nil {
					return "", err
				}
				if !keep {
					return value, nil
				}
			}
		}
		return "", fmt.Errorf("loop reached iteration limit without terminating")
	default:
		return "", fmt.Errorf("unsupported loop mode")
	}
}
