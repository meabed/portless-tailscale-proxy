package core

import "testing"

func TestQueryConfigParsesDockerFlag(t *testing.T) {
	_, dcfg, _ := queryConfig([]string{"--docker"})
	if !dcfg.docker {
		t.Fatal("queryConfig should enable Docker discovery from --docker")
	}
}
