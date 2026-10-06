package domain

// publicToEngine — мост public-имя поля формы → AAMVA engine-код.
// Используется для prepared-черновиков (в т.ч. LA auditCode → DCJ).
// Внимание: "state" намеренно НЕ мапится в DAJ — DAJ это идентичность ревизии
// (US_<STATE>_<DATE>), а не пользовательский ввод.
var publicToEngine = map[string]string{
	"firstName":             "DAC",
	"middleName":            "DAD",
	"lastName":              "DCS",
	"street":                "DAG",
	"city":                  "DAI",
	"zipCode":               "DAK",
	"country":               "DAL",
	"dateOfBirth":           "DBB",
	"idNumber":              "DAQ",
	"dlNumber":              "DAQ",
	"issueDate":             "DBD",
	"expiryDate":            "DBA",
	"expirationDate":        "DBA",
	"auditCode":             "DCJ",
	"weightPounds":          "DAW",
	"eyeColor":              "DAY",
	"hairColor":             "DAZ",
	"height":                "DAU",
	"sex":                   "DBC",
	"vehicleClass":          "DCA",
	"restrictions":          "DCB",
	"endorsements":          "DCD",
	"addressLine2":          "DAH",
	"veteran":               "DDL",
	"organDonor":            "DDK",
	"realId":                "DDA",
	"race":                  "DCL",
	"suffix":                "DCU",
	"county":                "ZGD",
	"safeDriver":            "ZFC",
	"documentDiscriminator": "DCF",
	"inventoryNumber":       "DCK",
}

// ApplyEngineDefaults заполняет отсутствующие engine-поля статическими
// константами профиля (ПЛАН §3.2 constants). Существующие непустые значения
// не перезаписываются; nil и пустая строка считаются «не задано».
func ApplyEngineDefaults(fields map[string]any, defaults map[string]string) {
	for code, value := range defaults {
		existing, exists := fields[code]
		if exists && existing != nil && existing != "" {
			continue
		}
		fields[code] = value
	}
}

// NormalizeEngineFields дополняет карту engine-кодами для public-имён (не
// перезаписывая уже заданные engine-ключи).
func NormalizeEngineFields(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	for k, v := range fields {
		code, ok := publicToEngine[k]
		if !ok {
			continue
		}
		if _, exists := out[code]; !exists {
			out[code] = v
		}
	}
	return out
}
