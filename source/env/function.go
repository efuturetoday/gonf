package env

import (
	"fmt"
	"os"

	"github.com/efuturetoday/gonf"
)

// FunctionName is the name of Function in ${env::NAME}.
const FunctionName = "env"

// Function returns the gonf.Function env, which reads any environment
// variable when it is called, also one that matches no key:
//
//	log.dir=${env::HOME}/logs
//	app.home=${app.dir:${env::HOME}}
//
// An unset variable is an error that names it; a set but empty one gives
// "". In ${app.home:${env::HOME}} the variable is read only if app.home
// has no value. The result is no secret: a key such as db.password is
// masked by its name, a field by gonf.Secret.
func Function() gonf.Function {
	return gonf.Function{Name: FunctionName, Call: lookupVariable}
}

func lookupVariable(_ *gonf.Config, name string, _ gonf.Origin) (string, error) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	return v, nil
}
