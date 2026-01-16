package utils

import (
	"bufio"
	"bytes"
	"log"
	"strings"
	"time"
)

const KeyHEAD = "HEAD"
const KeyTags = "tags"
const KeyBranch = "branch"
const KeyBuildDate = "buildDate"

type BuildVersion struct {
	raw       []byte              `json:"-"`
	data      map[string][]string `json:"-"`
	HEAD      *string             `json:"HEAD,omitempty"`
	Tags      []string            `json:"tags,omitempty"`
	Branch    *string             `json:"branch,omitempty"`
	BuildDate *time.Time          `json:"buildDate,omitempty"`
}

func NewBuildVersion(rawText []byte) (*BuildVersion, error) {

	bv := new(BuildVersion)
	bv.raw = make([]byte, len(rawText))
	copy(bv.raw, rawText)

	bv.data = make(map[string][]string)
	scanner := bufio.NewScanner(bytes.NewReader(rawText))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			log.Printf("skipping line: %s", line)
			continue
		}
		key := parts[0]
		vals := strings.Split(strings.TrimSpace(line[len(key)+1:]), " ")
		for _, val := range vals {
			val = strings.TrimSpace(val)
			if val == "" {
				continue
			}
			bv.data[key] = append(bv.data[key], val)
		}
	}

	if headStrs, ok := bv.data[KeyHEAD]; ok && len(headStrs) > 0 {
		headStr := strings.TrimSpace(headStrs[0])
		bv.HEAD = &headStr
	}

	if tagsStrs, ok := bv.data[KeyTags]; ok && len(tagsStrs) > 0 {
		bv.Tags = make([]string, len(tagsStrs))
		for i, tagsStr := range tagsStrs {
			bv.Tags[i] = strings.TrimSpace(tagsStr)
		}
	}

	if branchStrs, ok := bv.data[KeyBranch]; ok && len(branchStrs) > 0 {
		branchStr := strings.TrimSpace(branchStrs[0])
		bv.Branch = &branchStr
	}

	if buildDateStrs, ok := bv.data[KeyBuildDate]; ok && len(buildDateStrs) > 0 {
		buildDateStr := strings.TrimSpace(buildDateStrs[0])
		buildDate, err := time.Parse(time.RFC3339, buildDateStr)
		if err != nil {
			log.Printf("failed to parse build date: %s", buildDateStr)
		} else if !buildDate.IsZero() {
			bv.BuildDate = &buildDate
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("failed to scan build version: %v", err)
		return nil, err
	}

	return bv, nil
}
