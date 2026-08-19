package device

// NewWin 创建 Windows PC 端随机设备指纹（含 sofire zid 全链路，与 New 对齐）。
func NewWin() (*Device, error) {
	info := DeviceInfo{
		UserAgent:                 "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		Language:                  "zh-CN",
		ColorDepth:                "24",
		DeviceMemory:              "8",
		HardwareConcurrency:       "16",
		ScreenResolution:          "1920,1080",
		AvailableScreenResolution: "1040,1920",
		TimezoneOffset:            "-480",
		Timezone:                  "Asia/Shanghai",
		SessionStorage:            "true",
		LocalStorage:              "true",
		IndexedDb:                 "true",
		AddBehavior:               "false",
		OpenDatabase:              "false",
		CpuClass:                  "",
		Platform:                  "Win32",
		Plugins:                   "undefined",
		WebglVendorAndRenderer:    "Google Inc. (Intel)~ANGLE (Intel, Intel(R) UHD Graphics Direct3D11 vs_5_0 ps_5_0, D3D11)",
		AdBlock:                   "false",
		HasLiedLanguages:          "false",
		HasLiedResolution:         "false",
		HasLiedOs:                 "false",
		HasLiedBrowser:            "false",
		TouchSupport:              "0,false,false",
		Fonts:                     "33",
		Audio:                     "undefined",
	}
	info.Canvas = randomMD5()
	info.Webgl = randomMD5()

	fuid, err := generateFuid(info)
	if err != nil {
		return nil, err
	}

	// --- sofire zid 链路（与 New 一致）---
	cuid := generateCuid()
	xyus := genXyus()
	xyusec, err := genXyusec(xyus)
	if err != nil {
		return nil, err
	}
	zid30, err := genZid30(nil)
	if err != nil {
		return nil, err
	}
	zid65, err := genZid65(cuid, "")
	if err != nil {
		return nil, err
	}

	return &Device{
		Cuid:       cuid,
		Fuid:       fuid,
		Gid:        generateGid(),
		LogTraceID: generateLogTraceID(),
		PassID:     generatePassID(),
		Rinfo:      generateRinfo(fuid),
		Width:      1920,
		Height:     1040,
		Info:       info,
		Xyus:       xyus,
		Xyusec:     xyusec,
		Zid30:      zid30,
		Zid65:      zid65,
	}, nil
}
