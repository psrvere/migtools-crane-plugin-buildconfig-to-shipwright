package e2e

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/migtools/crane-plugin-buildconfig-to-shipwright/tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BuildConfig to Shipwright Conversion", func() {

	// Table-driven test: one entry per test directory
	// All tests validate: actual plugin output matches expected golden files
	DescribeTable("should convert BuildConfig to Shipwright Build correctly",
		func(testDir, description string) {
			// Setup paths
			testDirPath := filepath.Join(projectRoot, "tests", "testdata", testDir)
			buildConfigPath := filepath.Join(testDirPath, "buildconfig.yaml")
			flagsPath := filepath.Join(testDirPath, "flags.json")

			By(fmt.Sprintf("Running plugin on %s", testDir))

			// Load optional flags
			flags, err := framework.LoadOptionalFlags(flagsPath)
			Expect(err).NotTo(HaveOccurred())

			// Run plugin - returns ALL resources (Build, ServiceAccount, ConfigMap)
			resources, err := framework.RunPluginOnYAML(buildConfigPath, flags)
			Expect(err).NotTo(HaveOccurred())

			// Compare output with expected
			By(fmt.Sprintf("Comparing output (%d resource(s)) with expected", len(resources)))
			diffs, err := framework.CompareBuildsWithGoldenFile(resources, testDirPath)
			Expect(err).NotTo(HaveOccurred())

			if len(diffs) > 0 {
				Fail(fmt.Sprintf("Output differs from expected:\n  %s",
					strings.Join(diffs, "\n  ")))
			}

			// If expected_annotations.json exists, also validate annotations
			expectedAnnotationsPath := filepath.Join(testDirPath, "expected_annotations.json")
			expectedAnnotations, err := framework.LoadExpectedAnnotations(expectedAnnotationsPath)
			Expect(err).NotTo(HaveOccurred())

			if expectedAnnotations != nil {
				By("Validating outcome/reason annotations")

				// Get plugin response to check patches
				response, err := framework.RunPluginAndGetResponse(buildConfigPath, flags)
				Expect(err).NotTo(HaveOccurred())

				// Extract annotations from patches
				actualAnnotations, err := framework.ExtractAnnotationsFromPatches(response.Patches)
				Expect(err).NotTo(HaveOccurred())

				// Validate each expected annotation
				for key, expectedValue := range expectedAnnotations {
					actualValue, found := actualAnnotations[key]
					Expect(found).To(BeTrue(), fmt.Sprintf("Expected annotation %s not found", key))
					Expect(actualValue).To(Equal(expectedValue), fmt.Sprintf("Annotation %s mismatch", key))
				}
			}
		},

		// Test cases - one Entry per test directory
		// Format: Entry(label, testDir, description)
		// All tests validate: actual output matches expected golden files

		// Negative tests - Templates/Lists (plugin ignores non-BuildConfig CRs)
		Entry("template-negative", "21-template-negative", "Template ignored (negative test)"),
		Entry("list-negative", "22-list-negative", "List ignored (negative test)"),

		// Unsupported BuildConfigs - expected output: empty (rejected with annotations)
		Entry("jenkins-pipeline", "06-jenkins-pipeline", "JenkinsPipeline rejected"),
		Entry("custom-strategy", "07-custom-strategy", "Custom strategy rejected"),
		Entry("pullsecret-nodejs", "12-pullsecret-nodejs", "Missing output rejected"),

		// Successful conversions - expected output: Build resources
		Entry("datagrid-hotrod", "01-datagrid-hotrod", "S2I with triggers"),
		Entry("cakephp-mysql", "02-cakephp-mysql", "S2I with postCommit"),
		Entry("docker-and-s2i", "03-docker-and-s2i", "Multi-BuildConfig (2 Builds)"),
		Entry("webapp-docker", "04-webapp-docker", "Docker strategy"),
		Entry("api-s2i", "05-api-s2i", "S2I strategy"),
		Entry("docker-with-envvars", "08-docker-with-envvars", "Docker with envvars"),
		Entry("s2i-with-envvars", "09-s2i-with-envvars", "S2I with envvars"),
		Entry("docker-with-volumes", "10-docker-with-volumes", "Docker with volumes"),
		Entry("generic-trigger", "13-generic-test-build", "S2I with Generic trigger"),
		Entry("docker-postcommit", "14-docker-postcommit", "Docker with postCommit"),
		Entry("build-with-proxy", "15-build-with-proxy", "S2I with proxy"),
		Entry("cross-namespace-imagestream", "16-imagesource-cross-namespace", "Docker with cross-namespace ImageStream"),
		Entry("docker-nocache", "17-docker-nocache", "Docker nocache"),
		Entry("serviceaccount-override", "18-serviceaccount-override", "ServiceAccount override"),
		Entry("docker-imagestream-ruby", "19-docker-imagestream-ruby", "Docker with ImageStream"),
		Entry("s2i-imagestream-nodejs", "20-s2i-imagestream-nodejs", "S2I with ImageStream"),
		Entry("imagechange-trigger", "23-imagechange-trigger", "S2I with ImageChange trigger"),
		Entry("s2i-with-volumes", "11-s2i-with-volumes", "S2I with binary directory source and volumes"),
		Entry("binary-docker-certs", "24-binary-docker-certs", "Docker with binary directory source, source configMaps and secrets"),
		Entry("binary-asfile", "25-binary-asfile", "Docker with a single-file binary source (asFile)"),
	)
})
