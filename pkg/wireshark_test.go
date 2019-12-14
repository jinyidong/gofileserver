package pkg

import (
	"fmt"
	"regexp"
	"testing"
)

func TestBindUdIdAndFile(t *testing.T) {
	match, err := regexp.MatchString("/applesign/", "/applesign/ddddd")
	fmt.Println(match, err)
}
