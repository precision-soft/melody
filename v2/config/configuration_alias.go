package config

var environmentKeyAliasMap = map[string][]string{
	DefaultModeKey: {
		DefaultModeKey,
		KernelDefaultMode,
	},
	EnvKey: {
		EnvKey,
		KernelEnv,
	},
	HttpAddressKey: {
		HttpAddressKey,
		KernelHttpAddress,
	},
	CliNameKey: {
		CliNameKey,
		KernelCliName,
	},
	CliDescriptionKey: {
		CliDescriptionKey,
		KernelCliDescription,
	},
	LogPathKey: {
		LogPathKey,
		KernelLogPath,
	},
	LogLevelKey: {
		LogLevelKey,
		KernelLogLevel,
	},
	DefaultLocaleKey: {
		DefaultLocaleKey,
		KernelDefaultLocale,
	},
	PublicDirKey: {
		PublicDirKey,
		KernelPublicDir,
	},
	StaticIndexFileKey: {
		StaticIndexFileKey,
		KernelStaticIndexFile,
	},
	StaticEnableCacheKey: {
		StaticEnableCacheKey,
		KernelStaticEnableCache,
	},
	StaticCacheMaxAgeKey: {
		StaticCacheMaxAgeKey,
		KernelStaticCacheMaxAge,
	},
	HttpMaxRequestBodyBytesKey: {
		HttpMaxRequestBodyBytesKey,
		KernelHttpMaxRequestBodyBytes,
	},
}

func (instance *Configuration) addAliasedParameterFromEnvironment(
	parameterNames []string,
	environmentKey string,
	environmentValue string,
) error {
	if nil == parameterNames || 0 == len(parameterNames) {
		return nil
	}

	parameterInstance := NewParameter(
		environmentKey,
		environmentValue,
		environmentValue,
		false,
	)

	for _, name := range parameterNames {
		if "" == name {
			continue
		}

		instance.parameters[name] = parameterInstance
	}

	return nil
}

func (instance *Configuration) mapEnvironmentKeyToParameterNames(
	environmentKey string,
) []string {
	parameterNames, exists := environmentKeyAliasMap[environmentKey]
	if true == exists {
		return parameterNames
	}

	return []string{
		environmentKey,
	}
}
