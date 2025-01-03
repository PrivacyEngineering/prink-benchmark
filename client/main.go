package main

import (
	"log"
	"net"
	"os"
	"prinkbenchmarking/src/config"
	"prinkbenchmarking/src/evaluation"
	"prinkbenchmarking/src/exporter"
	"prinkbenchmarking/src/types"
	"strconv"
	"sync"
)

func experimentDone() {
	// Create experiment_done.txt (to be used later in tf script for stopping the cluster)
	file, err := os.Create("../results/experiment_done.txt")
	if err != nil {
		log.Fatalf("Could not create experiment_done.txt: %v", err)
	}
	defer file.Close()
}

func getExperiments() []types.Experiment {
	// k = [5,10,20,40,80]
	// delta = [1250,5000,20000,80000]
	// l diversity = [0,2,4,8] (wenn null bedeutet, dass l diversity nicht beachtet wird)
	// beta (active clusters). Damit das keinen Einfluss hat, so hoch setzen wie Daten: beta= 321728
	// mu= 100 (wie im original paper)

	k := []int{5, 10, 20, 40, 80}
	// 20000, 80000 take too long
	delta := []int{1250, 5000, 20000}
	l := []int{0, 2, 4, 8}
	beta := []int{321728}
	mu := []int{100}

	experiments := []types.Experiment{}

	for _, k := range k {
		for _, delta := range delta {
			for _, l := range l {
				for _, beta := range beta {
					for _, mu := range mu {
						experiment := types.Experiment{
							K:     k,
							Delta: delta,
							L:     l,
							Beta:  beta,
							Zeta:  0,
							Mu:    mu,
						}
						experiments = append(experiments, experiment)
					}
				}
			}
		}
	}

	return experiments
}

// Get preferred outbound ip of this machine
func GetOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}

func main() {
	// Load the config
	config := config.LoadConfig()

	localIP := config.LocalAddress
	if localIP == "" {
		localIP = GetOutboundIP().String()
	}

	log.Printf("Local IP: %s", localIP)

	// Start Prometheus exporter and register metrics
	go exporter.StartPrometheusExporter(config.PrometheusExporterAddress)

	if len(os.Args) > 1 {
		if os.Args[1] == "listen" {
			// just listen
			experiment := types.Experiment{
				K:            5,
				Delta:        20000,
				L:            0,
				Beta:         321728,
				Zeta:         0,
				Mu:           100,
				LocalHost:    localIP,
				SutHost:      config.SutAddresses[0],
				SutPortWrite: config.PortWrite,
				SutPortRead:  config.PortRead,
				RunId:        0,
			}

			evaluation.RunSockets(&experiment, *config)
			return
		}
	}

	// Experiment: k=80, delta=20000, l=0, beta=321728, zeta=0, mu=100, run_id=2, local_host=10.0.53.197, sut_host=10.0.34.49, sut_port_write=50069, sut_port_read=50070

	if len(os.Args) >= 3 {
		// Run a single experiment
		K, _ := strconv.Atoi(os.Args[1])
		Delta, _ := strconv.Atoi(os.Args[2])
		L, _ := strconv.Atoi(os.Args[3])

		experiment := types.Experiment{
			K:            K,
			Delta:        Delta,
			L:            L,
			Beta: 321728,
			Zeta: 0,
			Mu:   100,
			LocalHost: localIP,
			SutHost: config.SutAddresses[0],
			SutPortWrite: config.PortWrite,
			SutPortRead: config.PortRead,
			RunId: 0,
		}
		log.Print("Running in one-experiment mode")

		StartExperiments(localIP, config, []types.Experiment{experiment})
	}


	StartExperiments(localIP, config, nil)

	experimentDone()
	log.Printf("Created output files in: %s", config.OutputFolder)
}

func SetPrometheusTargets(addresses []string) {
	// Set the prometheus targets
	for target, port := range map[string]string{"jobmanager": "9249", "taskmanager": "9250"} {
		targets := ""
		for i, address := range addresses {
			targets += `{"targets":["` + address + `:` + port + `"],"labels":{"instance":"` + address + `:` + port + `","job":"prink"}}`
			if i < len(addresses)-1 {
				targets += ","
			}
		}
		targets = "[" + targets + "]"
		err := os.WriteFile("./prometheus/targets-"+target+".json", []byte(targets), 0666)
		if err != nil {
			log.Fatalf("Could not write targets.json: %v", err)
		}
	}
}

func StartExperiments(localIP string, config *types.Config, experiments []types.Experiment) {
	wg := sync.WaitGroup{}

	addresses := config.SutAddresses
	log.Println("Starting experiments on: ", addresses)

	// add addresses to targets.json
	SetPrometheusTargets(addresses)

	if experiments == nil {
		experiments = []types.Experiment{}
		for run := 0; run < 3; run++ {
			toAdd := []types.Experiment{}
			for _, e := range getExperiments() {
				e.RunId = run
				toAdd = append(toAdd, e)
			}
			experiments = append(experiments, toAdd...)
		}
	}

	exp := make(chan types.Experiment, len(experiments))
	for _, experiment := range experiments {
		exp <- experiment
	}

	wg.Add(len(addresses))

	for i, sutHost := range addresses {
		go func(i int, sutHost string) {
			for len(exp) > 0 {
				experiment := <-exp

				if experiment.Try >= 3 {
					log.Printf("Experiment %v failed 3 times. Skipping it.", experiment)
					continue
				}

				experiment.LocalHost = localIP
				experiment.SutHost = sutHost
				experiment.SutPortWrite = config.PortWrite + 2*i
				experiment.SutPortRead = config.PortRead + 2*i
				// Start the experiment
				log.Printf("Starting %d experiment on %s: %v", len(exp), sutHost, experiment)

				if evaluation.RunExperiment(experiment, *config) {
					log.Printf("Experiment %v finished successfully", experiment)
				} else {
					log.Printf("Experiment %v failed", experiment)
					// retry the experiment
					experiment.Try += 1
					exp <- experiment
				}
			}
			defer wg.Done()
		}(i, sutHost)
	}

	wg.Wait()

	if len(exp) > 0 {
		log.Printf("There are %d experiments left to run", len(exp))
	}
}
