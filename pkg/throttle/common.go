package throttle

func WrapWithSISOPipe(origin <-chan interface{}, pipe SISOPipe) <-chan interface{} {
	inC := pipe.GetInput()
	go func() {
		for pkt := range origin {
			inC <- pkt
		}
	}()
	return pipe.GetOutput()
}
