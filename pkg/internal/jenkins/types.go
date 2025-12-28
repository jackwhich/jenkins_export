// Package jenkins provides types and clients for interacting with Jenkins API.
package jenkins

// Build defines the response from specific builds.
type Build struct {
	Timestamp int64    `json:"timestamp"`
	Duration  int64    `json:"duration"`
	Result    string   `json:"result"`   // SUCCESS, FAILURE, ABORTED, UNSTABLE, null
	Building  bool     `json:"building"` // 是否正在构建
	QueueID   int64    `json:"queueId"`  // 队列ID（如果在队列中）
	Actions   []Action `json:"actions"`  // 包含参数信息
}

// Action defines an action in the build.
type Action struct {
	Class      string      `json:"_class"`
	Parameters []Parameter `json:"parameters,omitempty"`
	Causes     []Cause     `json:"causes,omitempty"`
}

// Parameter defines a build parameter.
type Parameter struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// Cause defines a build cause.
type Cause struct {
	Class            string `json:"_class"`
	ShortDescription string `json:"shortDescription"`
}

// Folder is a simple type used for folder listings.
type Folder struct {
	Class   string   `json:"_class"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Folders []Folder `json:"jobs"`
}

// Hudson defines the root type returned by the API.
type Hudson struct {
	Mode         string   `json:"mode"`
	NumExecutors int      `json:"numExecutors"`
	Folders      []Folder `json:"jobs"`
}

// BuildNumber defines a type for build numbers.
type BuildNumber struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// Job defines the response from specific jobs.
type Job struct {
	Class                 string       `json:"_class"`
	Name                  string       `json:"displayName"`
	Path                  string       `json:"fullName"`
	URL                   string       `json:"url"`
	Disabled              bool         `json:"disabled"`
	Buildable             bool         `json:"buildable"`
	Color                 string       `json:"color"`
	LastBuild             *BuildNumber `json:"lastBuild"`
	LastCompletedBuild    *BuildNumber `json:"lastCompletedBuild"`
	LastFailedBuild       *BuildNumber `json:"lastFailedBuild"`
	LastStableBuild       *BuildNumber `json:"lastStableBuild"`
	LastSuccessfulBuild   *BuildNumber `json:"lastSuccessfulBuild"`
	LastUnstableBuild     *BuildNumber `json:"lastUnstableBuild"`
	LastUnsuccessfulBuild *BuildNumber `json:"lastUnsuccessfulBuild"`
	NextBuildNumber       int          `json:"nextBuildNumber"`
}
