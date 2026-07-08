package habridge

// topics builds every MQTT topic the bridge uses from a single base root, so the
// discovery payloads and the command router agree on the layout.
//
//	<base>/availability
//	<base>/volume/state   <base>/volume/set
//	<base>/power/state    <base>/power/set
//	<base>/preset/state   <base>/preset/set
//	<base>/transport/<key>/set          (button: play|pause|stop|next|prev)
//	<base>/update/state  <base>/update/install   (update entity)
//	<base>/station  <base>/track  <base>/artist  <base>/status
//	<base>/sw_version  <base>/model
type topics struct{ base string }

func (t topics) availability() string { return t.base + "/availability" }

func (t topics) volumeState() string { return t.base + "/volume/state" }
func (t topics) volumeSet() string   { return t.base + "/volume/set" }

func (t topics) powerState() string { return t.base + "/power/state" }
func (t topics) powerSet() string   { return t.base + "/power/set" }

func (t topics) presetState() string { return t.base + "/preset/state" }
func (t topics) presetSet() string   { return t.base + "/preset/set" }

func (t topics) transportSet(key string) string { return t.base + "/transport/" + key + "/set" }

func (t topics) updateState() string   { return t.base + "/update/state" }
func (t topics) updateInstall() string { return t.base + "/update/install" }

func (t topics) station() string   { return t.base + "/station" }
func (t topics) track() string     { return t.base + "/track" }
func (t topics) artist() string    { return t.base + "/artist" }
func (t topics) status() string    { return t.base + "/status" }
func (t topics) swVersion() string { return t.base + "/sw_version" }
func (t topics) model() string     { return t.base + "/model" }
