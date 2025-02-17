// Code generated by "stringer -type=SensorDeviceClass -output deviceClassStrings.go -trimprefix Sensor"; DO NOT EDIT.

package sensor

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Apparent_power-1]
	_ = x[Aqi-2]
	_ = x[Atmospheric_pressure-3]
	_ = x[SensorBattery-4]
	_ = x[Carbon_dioxide-5]
	_ = x[Carbon_monoxide-6]
	_ = x[Current-7]
	_ = x[Data_rate-8]
	_ = x[Data_size-9]
	_ = x[Date-10]
	_ = x[Distance-11]
	_ = x[Duration-12]
	_ = x[Energy-13]
	_ = x[EnergyStorage-14]
	_ = x[Enum-15]
	_ = x[Frequency-16]
	_ = x[Gas-17]
	_ = x[Humidity-18]
	_ = x[Illuminance-19]
	_ = x[Irradiance-20]
	_ = x[Moisture-21]
	_ = x[Monetary-22]
	_ = x[Nitrogen_dioxide-23]
	_ = x[Nitrogen_monoxide-24]
	_ = x[Nitrous_oxide-25]
	_ = x[Ozone-26]
	_ = x[Pm1-27]
	_ = x[Pm25-28]
	_ = x[Pm10-29]
	_ = x[Power_factor-30]
	_ = x[SensorPower-31]
	_ = x[Precipitation-32]
	_ = x[Precipitation_intensity-33]
	_ = x[Pressure-34]
	_ = x[Reactive_power-35]
	_ = x[Signal_strength-36]
	_ = x[Sound_pressure-37]
	_ = x[Speed-38]
	_ = x[Sulphur_dioxide-39]
	_ = x[SensorTemperature-40]
	_ = x[Timestamp-41]
	_ = x[Volatile_organic_compounds-42]
	_ = x[Voltage-43]
	_ = x[Volume-44]
	_ = x[Water-45]
	_ = x[Weight-46]
	_ = x[Wind_speed-47]
}

const _SensorDeviceClass_name = "Apparent_powerAqiAtmospheric_pressureBatteryCarbon_dioxideCarbon_monoxideCurrentData_rateData_sizeDateDistanceDurationEnergyEnergyStorageEnumFrequencyGasHumidityIlluminanceIrradianceMoistureMonetaryNitrogen_dioxideNitrogen_monoxideNitrous_oxideOzonePm1Pm25Pm10Power_factorPowerPrecipitationPrecipitation_intensityPressureReactive_powerSignal_strengthSound_pressureSpeedSulphur_dioxideTemperatureTimestampVolatile_organic_compoundsVoltageVolumeWaterWeightWind_speed"

var _SensorDeviceClass_index = [...]uint16{0, 14, 17, 37, 44, 58, 73, 80, 89, 98, 102, 110, 118, 124, 137, 141, 150, 153, 161, 172, 182, 190, 198, 214, 231, 244, 249, 252, 256, 260, 272, 277, 290, 313, 321, 335, 350, 364, 369, 384, 395, 404, 430, 437, 443, 448, 454, 464}

func (i SensorDeviceClass) String() string {
	i -= 1
	if i < 0 || i >= SensorDeviceClass(len(_SensorDeviceClass_index)-1) {
		return "SensorDeviceClass(" + strconv.FormatInt(int64(i+1), 10) + ")"
	}
	return _SensorDeviceClass_name[_SensorDeviceClass_index[i]:_SensorDeviceClass_index[i+1]]
}
