package server

type paginationMeta struct {
	Total      int  `json:"total"`
	Count      int  `json:"count"`
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	NextOffset *int `json:"next_offset,omitempty"`
	PrevOffset *int `json:"prev_offset,omitempty"`
}

type paginatedResponse[T any] struct {
	Data       []T            `json:"data"`
	Pagination paginationMeta `json:"pagination"`
}

func newPaginationMeta(total, limit, offset, count int) paginationMeta {
	meta := paginationMeta{
		Total:  total,
		Count:  count,
		Limit:  limit,
		Offset: offset,
	}

	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		meta.PrevOffset = paginationOffsetPtr(prev)
	}

	if limit > 0 && offset+count < total {
		next := offset + limit
		if next < total {
			meta.NextOffset = paginationOffsetPtr(next)
		}
	}

	return meta
}

func paginationOffsetPtr(value int) *int {
	v := value
	return &v
}
