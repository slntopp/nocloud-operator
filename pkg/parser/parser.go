package parser

func (w *MapOrArrayWrapper) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var envsArray []string
	var envsMap map[string]string
	if err := unmarshal(&envsMap); err == nil {
		for key, val := range envsMap {
			envsArray = append(envsArray, key+"="+val)
		}
	}

	if len(envsArray) == 0 {
		if err := unmarshal(&envsArray); err != nil {
			return err
		}
	}
	*w = envsArray
	return nil
}
