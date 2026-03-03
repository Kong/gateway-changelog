package utils

import (
	"os"
	"regexp"
)

func MatchJiras(str string) []string {
	r := regexp.MustCompile(`\b(?:FTI|AG|KAG|KM|K8|OLLY|KOKO)-\d{1,6}\b`)
	return r.FindAllString(str, -1)
}

func DirExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
