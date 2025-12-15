package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func ParseInts(s string) ([]int, error) {
	results := make([]int, 0)

	// e.g. range(1;10) will generate [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
	pattern1 := regexp.MustCompile(`^range\((\d+);\s*(\d+)\)$`)
	if result := pattern1.FindStringSubmatch(s); result != nil {
		start, err := strconv.Atoi(result[1])
		if err != nil {
			return nil, err
		}
		end, err := strconv.Atoi(result[2])
		if err != nil {
			return nil, err
		}
		for i := start; i <= end; i += 1 {
			results = append(results, i)
		}
		if len(results) == 0 {
			return nil, fmt.Errorf("no results found")
		}
		return results, nil
	}

	// range(start;end;step) will generate [start, start+step, start+2*step, ...]
	pattern2 := regexp.MustCompile(`^range\((\d+);\s*(\d+);\s*(\d+)\)`)

	if result := pattern2.FindStringSubmatch(s); result != nil {
		start, err := strconv.Atoi(result[1])
		if err != nil {
			return nil, err
		}
		end, err := strconv.Atoi(result[2])
		if err != nil {
			return nil, err
		}
		step, err := strconv.Atoi(result[3])
		if err != nil {
			return nil, err
		}
		for i := start; i <= end; i += step {
			results = append(results, i)
		}
		if len(results) == 0 {
			return nil, fmt.Errorf("no results found")
		}
		return results, nil
	}

	segs := strings.Split(s, ",")
	for _, seg := range segs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		num, err := strconv.Atoi(seg)
		if err != nil {
			return nil, err
		}
		results = append(results, num)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results found")
	}
	return results, nil
}
