package media

type Chapter struct {
	Start Scalar            `json:"start_time"`
	End   Scalar            `json:"end_time"`
	Tags  map[string]string `json:"tags"`
}
