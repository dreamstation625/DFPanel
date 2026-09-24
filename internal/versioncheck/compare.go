package versioncheck

import (
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.-]+))?$`)

type parsedVersion struct {
	core [3]uint64
	pre  []string
}

func parseVersion(value string) (parsedVersion, bool) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return parsedVersion{}, false
	}
	var parsed parsedVersion
	for index := range parsed.core {
		number, err := strconv.ParseUint(match[index+1], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		parsed.core[index] = number
	}
	if match[4] != "" {
		parsed.pre = strings.Split(match[4], ".")
		for _, part := range parsed.pre {
			if part == "" {
				return parsedVersion{}, false
			}
		}
	}
	return parsed, true
}

// IsPrerelease 区分正式版与带 beta、rc 等尾缀的预发布版。
func IsPrerelease(value string) (isPrerelease bool, valid bool) {
	parsed, ok := parseVersion(value)
	return len(parsed.pre) > 0, ok
}

// Compare 比较面板与 Agent 的程序版本；无法识别 dev 等非发布版本时返回 ok=false。
func Compare(left, right string) (order int, ok bool) {
	a, validA := parseVersion(left)
	b, validB := parseVersion(right)
	if !validA || !validB {
		return 0, false
	}
	for index := range a.core {
		if a.core[index] > b.core[index] {
			return 1, true
		}
		if a.core[index] < b.core[index] {
			return -1, true
		}
	}
	if len(a.pre) == 0 && len(b.pre) > 0 {
		return 1, true
	}
	if len(a.pre) > 0 && len(b.pre) == 0 {
		return -1, true
	}
	for index := 0; index < len(a.pre) && index < len(b.pre); index++ {
		leftNumber, leftErr := strconv.ParseUint(a.pre[index], 10, 64)
		rightNumber, rightErr := strconv.ParseUint(b.pre[index], 10, 64)
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber > rightNumber {
				return 1, true
			}
			if leftNumber < rightNumber {
				return -1, true
			}
		case leftErr == nil:
			return -1, true
		case rightErr == nil:
			return 1, true
		default:
			if order := strings.Compare(a.pre[index], b.pre[index]); order != 0 {
				return order, true
			}
		}
	}
	if len(a.pre) > len(b.pre) {
		return 1, true
	}
	if len(a.pre) < len(b.pre) {
		return -1, true
	}
	return 0, true
}
