package gql

import "strings"

func IsErrorNotFound(err error) bool {
	return strings.Contains(err.Error(), "not find")
}
