package storageurl

// ValidAzureAccount reports whether s is a storage account name.
func ValidAzureAccount(s string) bool { return azAccountRE.MatchString(s) }
