package app

type Data struct {
	Port int
}

func New(port int) *Data {

	return &Data{
		Port: port,
	}
}
