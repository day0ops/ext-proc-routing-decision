package config

import "os"

var LogLevel = os.Getenv("LOG_LEVEL")
var RoutingDecisionServer = os.Getenv("ROUTING_DECISION_SERVER")
