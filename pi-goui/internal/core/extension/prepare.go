package extension

import "context"

// Preparation runs against a captured writer. Commit is called on the UI loop
// only if that writer still belongs to the originating execution cycle.
type Preparation func(context.Context) (commit func(), err error)

type backendInitializer interface{ Prepare(Client) Preparation }

func (s Sessions) Prepare(client Client) Preparation {
	var steps []Preparation
	for _, v := range s {
		if b, ok := v.(backendInitializer); ok {
			if f := b.Prepare(client); f != nil {
				steps = append(steps, f)
			}
		}
	}
	if len(steps) == 0 {
		return nil
	}
	return func(ctx context.Context) (func(), error) {
		var commits []func()
		for _, f := range steps {
			c, err := f(ctx)
			if err != nil {
				return nil, err
			}
			if c != nil {
				commits = append(commits, c)
			}
		}
		return func() {
			for _, c := range commits {
				c()
			}
		}, nil
	}
}
