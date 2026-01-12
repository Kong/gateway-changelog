package utils

import (
	"os"
	"regexp"
)

func MatchJiras(str string) []string {
	r := regexp.MustCompile(`[A-Z]+-\d+`)
	jiras := make([]string, 0)
	strs := r.FindAllString(str, -1)
	r1 := regexp.MustCompile(`^[A-Z]{2,8}-\d{1,6}$`)
	for _, str := range strs {
		if r1.MatchString(str) {
			jiras = append(jiras, str)
		}
	}
	return jiras
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
