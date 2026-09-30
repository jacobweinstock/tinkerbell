package flag

import (
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/tinkerbell/tinkerbell/hardware"
)

type HardwareControllerConfig struct {
	Config   *hardware.Config
	LogLevel int
}

func RegisterHardwareControllerFlags(fs *Set, h *HardwareControllerConfig) {
	fs.Register(HardwareControllerEnableLeaderElection, ffval.NewValueDefault(&h.Config.EnableLeaderElection, h.Config.EnableLeaderElection))
	fs.Register(HardwareControllerLeaderElectionNamespace, ffval.NewValueDefault(&h.Config.LeaderElectionNamespace, h.Config.LeaderElectionNamespace))
	fs.Register(HardwareControllerLogLevel, ffval.NewValueDefault(&h.LogLevel, h.LogLevel))
}

var HardwareControllerEnableLeaderElection = Config{
	Name:  "hardware-controller-enable-leader-election",
	Usage: "enable leader election for controller manager",
}

var HardwareControllerLeaderElectionNamespace = Config{
	Name:  "hardware-controller-leader-election-namespace",
	Usage: "namespace in which the leader election lease will be created",
}

var HardwareControllerLogLevel = Config{
	Name:  "hardware-controller-log-level",
	Usage: logLevelUsage,
}
