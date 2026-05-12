package projecttype

func init() {
	Register(&ProjectType{
		Name:        "go",
		Description: "Go module (go.mod)",
		Indicators:  []Indicator{{HasFile: "go.mod"}},
	})

	Register(&ProjectType{
		Name:        "node",
		Description: "Node.js / npm / yarn / pnpm (package.json)",
		Indicators:  []Indicator{{HasFile: "package.json"}},
	})

	Register(&ProjectType{
		Name:        "rust",
		Description: "Rust crate (Cargo.toml)",
		Indicators:  []Indicator{{HasFile: "Cargo.toml"}},
	})

	Register(&ProjectType{
		Name:        "python",
		Description: "Python project (pyproject.toml / requirements.txt / Pipfile / setup.py / setup.cfg)",
		Indicators: []Indicator{
			{HasFile: "pyproject.toml"},
			{HasFile: "requirements.txt"},
			{HasFile: "Pipfile"},
			{HasFile: "setup.py"},
			{HasFile: "setup.cfg"},
		},
	})

	Register(&ProjectType{
		Name:        "ruby",
		Description: "Ruby Bundler project (Gemfile)",
		Indicators:  []Indicator{{HasFile: "Gemfile"}},
	})

	Register(&ProjectType{
		Name:        "java-maven",
		Description: "Java Maven project (pom.xml)",
		Indicators:  []Indicator{{HasFile: "pom.xml"}},
	})

	Register(&ProjectType{
		Name:        "java-gradle",
		Description: "Java/Kotlin Gradle project (build.gradle / build.gradle.kts)",
		Indicators: []Indicator{
			{HasFile: "build.gradle"},
			{HasFile: "build.gradle.kts"},
			{HasFile: "settings.gradle"},
			{HasFile: "settings.gradle.kts"},
		},
	})

	Register(&ProjectType{
		Name:        "dotnet",
		Description: ".NET project (*.csproj / *.fsproj / *.vbproj)",
		Indicators: []Indicator{
			{HasGlob: "*.csproj"},
			{HasGlob: "*.fsproj"},
			{HasGlob: "*.vbproj"},
			{HasGlob: "*.sln"},
		},
	})

	Register(&ProjectType{
		Name:        "terraform",
		Description: "Terraform / OpenTofu (*.tf)",
		Indicators:  []Indicator{{HasGlob: "*.tf"}},
	})

	Register(&ProjectType{
		Name:        "docker-compose",
		Description: "Docker Compose stack (docker-compose.{yml,yaml} / compose.{yml,yaml})",
		Indicators: []Indicator{
			{HasFile: "docker-compose.yml"},
			{HasFile: "docker-compose.yaml"},
			{HasFile: "compose.yml"},
			{HasFile: "compose.yaml"},
		},
	})
}
