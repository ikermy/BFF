package legacy

// Словари соответствия полей BFF ↔ AAMVA-аббревиатуры BarcodeGen.
//
// BFF-конфиги ревизий (configs/revisions/*.yaml) оперируют человекочитаемыми именами
// (firstName, street, ...), а BarcodeGen принимает/возвращает только AAMVA-коды
// (ValuesDto: DAJ, DDB, DAC, ...). Эти таблицы — мост между ними (отчёт §4.2 п.2).

// humanToAAMVA — прямое соответствие человекочитаемого имени поля → AAMVA-код.
// Покрывает поля, используемые в конфигах ревизий.
var humanToAAMVA = map[string]string{
	"firstName":      "DAC",
	"middleName":     "DAD",
	"lastName":       "DCS",
	"street":         "DAG",
	"city":           "DAI",
	"state":          "DAJ",
	"zipCode":        "DAK",
	"country":        "DAL",
	"dateOfBirth":    "DBB",
	"idNumber":       "DAQ",
	"dlNumber":       "DAQ",
	"issueDate":      "DBD",
	"expiryDate":     "DBA",
	"expirationDate": "DBA",
}

// aamvaToHuman — обратное соответствие AAMVA-код → человекочитаемое имя.
var aamvaToHuman = map[string]string{
	"DAC": "firstName",
	"DAD": "middleName",
	"DCS": "lastName",
	"DAG": "street",
	"DAI": "city",
	"DAJ": "state",
	"DAK": "zipCode",
	"DAL": "country",
	"DBB": "dateOfBirth",
	"DAQ": "dlNumber",
	"DBD": "issueDate",
	"DBA": "expiryDate",
}

// valuesWhitelist — допустимые ключи в ValuesDto BarcodeGen
// (POST /barcodes/pdf417 с forbidNonWhitelisted). Всё, что вне списка, → 400.
// Это ключи, которые адаптер вправе вбросить в values при генерации PDF417.
var valuesWhitelist = map[string]bool{
	"DAJ": true, "DDB": true, "DAC": true, "DAD": true, "DCS": true,
	"DAG": true, "DAI": true, "DAK": true, "DAL": true, "DAQ": true,
	"DBD": true, "DBA": true, "DBB": true, "DCJ": true, "DCF": true,
	"DCK": true, "DAH": true, "DAY": true, "DAU": true, "DCE": true,
	"DDA": true, "DBG": true, "DBH": true,
}

// labelToAAMVA — обратная таблица «человекочитаемый лейбл ответа random/calculate
// (из field_names.json, с двоеточием)» → AAMVA-код. Нужна для реверс-маппинга
// ответов random/calculate, которые приходят с ключами-лейблами вида "Street:".
var labelToAAMVA = map[string]string{
	"First Name:": "DAC", "Middle Name:": "DAD", "Last Name:": "DCS",
	"Street:": "DAG", "City:": "DAI", "State:": "DAJ", "ZIP Code:": "DAK",
	"Country:": "DAL", "Date of Birth:": "DBB", "DL Number:": "DAQ",
	"Issue Date:": "DBD", "Expiration Date:": "DBA",
}

// toAAMVA переводит человекочитаемое имя поля в AAMVA-код.
// Если имя уже является AAMVA-кодом (3 заглавные буквы + цифра) — возвращается как есть.
func toAAMVA(field string) string {
	if code, ok := humanToAAMVA[field]; ok {
		return code
	}
	if isAAMVA(field) {
		return field
	}
	return field
}

// toHuman переводит AAMVA-код в человекочитаемое имя. Если код неизвестен —
// возвращается как есть.
func toHuman(code string) string {
	if name, ok := aamvaToHuman[code]; ok {
		return name
	}
	return code
}

// isAAMVA возвращает true, если строка похожа на AAMVA-код (3 заглавные буквы + цифра).
func isAAMVA(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i, r := range s {
		if i < 2 && (r < 'A' || r > 'Z') {
			return false
		}
		if i == 2 && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// mapFields переводит карту полей (человекочитаемые имена) в карту AAMVA-кодов,
// фильтруя строго по whitelist ValuesDto (иначе forbidNonWhitelisted вернёт 400).
// Ключи, не являющиеся ни человекочитаемым именем, ни валидным AAMVA-кодом из whitelist,
// отбрасываются.
func mapFields(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		code := toAAMVA(k)
		if !valuesWhitelist[code] {
			continue
		}
		if _, exists := out[code]; !exists {
			out[code] = v
		}
	}
	return out
}
