package values

import (
	"github.com/jinyongp/devtools/internal/dotenv"
	"github.com/jinyongp/devtools/internal/protocol"
)

func ParseDotenv(input string) (map[string]string, *protocol.Error) { return dotenv.ParseDotenv(input) }
