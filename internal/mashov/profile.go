package mashov

// Profile describes the app identity Mashov sees. The Android profile is the
// default (it is what the app sends); the web profile is kept for Phase 1
// verification of which one issues devicePass (CLAUDE.md §3.2).
type Profile struct {
	Name               string
	UserAgent          string
	AppName            string
	APIVersion         string
	AppVersion         string
	AppBuild           string
	DevicePlatform     string // Android profile only
	DeviceManufacturer string // Android profile only
	DeviceModel        string // Android profile only
	DeviceVersion      string // Android profile only
}

// AndroidProfile mirrors com.mashov.main 3.8.72 running in an Android WebView.
var AndroidProfile = Profile{
	Name:               "android",
	UserAgent:          "Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/124.0.0.0 Mobile Safari/537.36",
	AppName:            "com.mashov.main",
	APIVersion:         "3.20210425",
	AppVersion:         "3.8.72",
	AppBuild:           "308720",
	DevicePlatform:     "Android",
	DeviceManufacturer: "Google",
	DeviceModel:        "Pixel 7",
	DeviceVersion:      "14",
}

// WebProfile mirrors the web client (info.mashov.students). It sends no device fields.
var WebProfile = Profile{
	Name:       "web",
	UserAgent:  "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	AppName:    "info.mashov.students",
	APIVersion: "3.20210425",
	AppVersion: "3.8.72",
	AppBuild:   "308720",
}

// DeviceFields returns the login device identity for this profile. Fields the
// profile leaves empty (the web profile has no device fields) are omitted.
func (p Profile) DeviceFields(deviceUUID string) DeviceFields {
	return DeviceFields{
		AppName: p.AppName, APIVersion: p.APIVersion, AppVersion: p.AppVersion, AppBuild: p.AppBuild,
		DeviceUUID: deviceUUID, DevicePlatform: p.DevicePlatform, DeviceManufacturer: p.DeviceManufacturer,
		DeviceModel: p.DeviceModel, DeviceVersion: p.DeviceVersion,
	}
}
