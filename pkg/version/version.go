package version

import "fmt"

var (
	Name      = "unset"
	GitCommit = "unset"
	Version   string

	HumanVersion = fmt.Sprintf("%s:%s (%s)", Name, Version, GitCommit)
)
