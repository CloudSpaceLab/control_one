package doris

import "strconv"

func withLimit(query string, limit int) string {
	return query + "\n\t\tLIMIT " + strconv.Itoa(limit)
}

func withLimitOffset(query string, limit, offset int) string {
	return query + " LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset)
}
