package master

type addons struct{}

func (a *addons) Start() error {
	err := startTimeSync()
	return err
}

func (a *addons) Runtime() error {
	err := runtimeTimeSync()
	return err
}

func (a *addons) Stop() error {
	err := stopTimeSync()
	return err
}
