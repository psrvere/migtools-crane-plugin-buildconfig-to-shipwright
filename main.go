package main

import (
	"github.com/migtools/crane-plugin-buildconfig-to-builds/buildconfig"

	"github.com/konveyor/crane-lib/transform/cli"
	"github.com/sirupsen/logrus"
)

func main() {
	plugin := &buildconfig.BuildConfigTransformPlugin{
		Log: logrus.New(),
	}
	meta := plugin.Metadata()
	cli.RunAndExit(cli.NewCustomPlugin(meta.Name, buildconfig.PluginVersion, meta.OptionalFields, plugin.Run))
}
