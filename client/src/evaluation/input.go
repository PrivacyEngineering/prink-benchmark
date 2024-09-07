package evaluation

import (
	"fmt"
	"net"
	"prinkbenchmarking/src/types"
)


func socketConnection(e *types.Experiment, dataset [][]string) error {
	// Open socket connection
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", "0.0.0.0", e.SutPortWrite))
	if err != nil {
		return fmt.Errorf("could not open socket connection: %v", err)
	}
	defer ln.Close()

	// Accept connection
	conn, err := ln.Accept()
	if err != nil {
		return fmt.Errorf("could not accept connection: %v", err)
	}
	defer conn.Close()

	// Handle connection
	return benchmark(dataset, conn)
}