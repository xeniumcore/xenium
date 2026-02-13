package ports

type Clock interface {
	UnixNano() int64
}
