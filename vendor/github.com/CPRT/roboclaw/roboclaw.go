//Created by Michael Dysart
package roboclaw

import (
	"fmt"
	"github.com/tarm/serial"
	"io"
	"time"
)

// A structure for describing the roboclaw interface
type Roboclaw struct {
	port       *serial.Port
	retries    uint8
	writeSleep bool
}

type Config struct {
	Name       string //the name of the serial port
	Baud       int    // the baud rate for the serial port
	Retries    uint8  // the number of attempted retries for sending a command to the roboclaws
	WriteSleep bool   // The timeout for the roboclaws is 10 milliseconds
	// However, some operating systems have a minimum timeout above this value
	// For certain applications, it is inefficient to have to wait for a reply to
	// a write command (such as setting motor speed) if no remedial action is
	// possible or planned if the write fails. For example, in the situation
	// where the tx line to the roboclaw is functional but the rx line has failed,
	// it is often desired to still be able to write to the roboclaws
	// but innefficient to wait for the non-existent reply. The WriteSleep
	// flag causes the system to wait 10 milliseconds after writing the command to the rover
	// instead of the full time necessary to wait for the reply (which is not expected to come).
	// This flag does not affect any commands that involve reading from the roboclaws or expected
	// any reply other than the acknoweldgement 0xFF.
}

/*
 * Initialize the roboclaw with the desired serial port
 * @param rc (&Config) the roboclaw config struct
 * @returns (*Roboclaw, error) the reference to the roboclaw and any errors that occur when opening the file
 */
func Init(rc *Config) (*Roboclaw, error) {
	// The expected timeout for a roboclaw to reply to a message is 10 milliseconds
	// However, some operating systems have a minimum timeout above this value
	// For example, on a linux system the minimum timeout for a serial port is 100 milliseconds
	c := &serial.Config{Name: rc.Name, Baud: rc.Baud, ReadTimeout: time.Millisecond * 10}

	if rc.Retries == 0 {
		return nil, fmt.Errorf("Number of retries must be larger than zero")
	} else if port, err := serial.OpenPort(c); err == nil {
		return &Roboclaw{port: port, retries: rc.Retries, writeSleep: rc.WriteSleep}, err
	} else {
		return nil, err
	}
}

/*
 * Close the roboclaw's serial port
 * @return {error}
 */
func (r *Roboclaw) Close() error {
	return r.port.Close()
}

// See the roboclaw user manual
// at http://downloads.ionmc.com/docs/roboclaw_user_manual.pdf
// for more details

/*
 * Drive motor 1 forwards
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127
 * @return {error}
 */
func (r *Roboclaw) ForwardM1(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 0, speed)
}

/*
 * Drive motor 1 backwards
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127
 * @return {error}
 */
func (r *Roboclaw) BackwardM1(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 1, speed)
}

/*
 * Drive motor 2 forwards
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127
 * @return {error}
 */
func (r *Roboclaw) ForwardM2(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 4, speed)
}

/*
 * Drive motor 2 backwards
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127
 * @return {error}
 */
func (r *Roboclaw) BackwardM2(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 5, speed)
}

/*
 * Sets minimum main voltage (command 57 preferred)
 * Roboclaw shuts down if voltage drops below this point
 * Volts are (Desired volts - 6) * 5
 * @param address {uint8}
 * @param voltage {uint8} valid data is 0 to 140 (6V to 34 V)
 * @return {error}
 */
func (r *Roboclaw) SetMinVoltageMainBattery(address uint8, voltage uint8) error {
	if voltage > 140 {
		return fmt.Errorf("Voltage above maximum of 140")
	}
	return r.write_n(address, 2, voltage)
}

/*
 * Sets maximum main voltage (command 57 preferred)
 * Roboclaw shuts down if voltage drops below this point
 * Volts are Desired volts *  5.12
 * @param address {uint8}
 * @param voltage {uint8} valid data is 30 to 175 (6V to 34 V)
 * @return {error}
 */
func (r *Roboclaw) SetMaxVoltageMainBattery(address uint8, voltage uint8) error {
	if voltage > 175 {
		return fmt.Errorf("Voltage above maximum of 175")
	} else if voltage < 30 {
		return fmt.Errorf("Voltage below minimum of 30")
	}
	return r.write_n(address, 3, voltage)
}

/*
 * Drive motor 1 forward or backward
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 full reverse, 64 stop, 127 full forward)
 * @return {error}
 */
func (r *Roboclaw) ForwardBackwardM1(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 6, speed)
}

/*
 * Drive motor 2 forward or backward
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 full reverse, 64 stop, 127 full forward)
 * @return {error}
 */
func (r *Roboclaw) ForwardBackwardM2(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 7, speed)
}

/*
 * Drive motors forward in mixed mode
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 stop, 127 full speed)
 * @return {error}
 */
func (r *Roboclaw) ForwardMixed(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 8, speed)
}

/*
 * Drive motors backward in mixed mode
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 stop, 127 full speed)
 * @return {error}
 */
func (r *Roboclaw) BackwardMixed(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 9, speed)
}

/*
 * Turn right in mixed mode
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 stop, 127 full speed)
 * @return {error}
 */
func (r *Roboclaw) TurnRightMixed(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 10, speed)
}

/*
 * Turn left in mixed mode
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 stop, 127 full speed)
 * @return {error}
 */
func (r *Roboclaw) TurnLeftMixed(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 11, speed)
}

/*
 * Drive forward or backward in mixed mode
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 full speed backward, 0 stop, 127 full forward)
 * @return {error}
 */
func (r *Roboclaw) ForwardBackwardMixed(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 12, speed)
}

/*
 * Turn in mixed mode
 * @param address {uint8}
 * @param speed {uint8} valid data is 0 to 127 (0 full speed left, 0 stop, 127 full right)
 * @return {error}
 */
func (r *Roboclaw) LeftRightMixed(address uint8, speed uint8) error {
	if speed > 127 {
		return fmt.Errorf("Speed above maximum of 127")
	}
	return r.write_n(address, 13, speed)
}

/*
 * Read the encoder count for m1
 * @param address {uint8}
 * @return {uint32, uint8, error} encoder count, encoder status, error
 */
func (r *Roboclaw) ReadEncM1(address uint8) (uint32, uint8, error) {
	return r.read4_1(address, 16)
}

/*
 * Read the encoder count for m2
 * @param address {uint8}
 * @return {uint32, uint8, error} encoder count, encoder status, error
 */
func (r *Roboclaw) ReadEncM2(address uint8) (uint32, uint8, error) {
	return r.read4_1(address, 17)
}

/*
 * Read the encoder speed for m1
 * Speed is in pulses per second
 * Direction is 0 for forward and 1 for backward
 * @param address {uint8}
 * @return {uint32, uint8, error} encoder speed, direction, error
 */
func (r *Roboclaw) ReadSpeedM1(address uint8) (uint32, uint8, error) {
	return r.read4_1(address, 18)
}

/*
 * Read the encoder speed for m2
 * Speed is in pulses per second
 * Direction is 0 for forward and 1 for backward
 * @param address {uint8}
 * @return {uint32, uint8, error} encoder speed, direction, error
 */
func (r *Roboclaw) ReadSpeedM2(address uint8) (uint32, uint8, error) {
	return r.read4_1(address, 19)
}

/*
 * Reset encoder counters to 0 (for quadrature encoders)
 * @param address {uint8}
 * @return {error}
 */
func (r *Roboclaw) ResetEncoders(address uint8) error {
	return r.write_n(address, 20)
}

/*
 * Read the roboclaw version
 * @param address {uint8}
 * @return {string, error}
 */
func (r *Roboclaw) ReadVersion(address uint8) (string, error) {

	var (
		version []uint8 = make([]uint8, 0)
		crc     crcType
		ccrc    uint16
		buf     [2]uint8
		err     error
	)

loop:
	for trys := r.retries; trys > 0; trys-- {
		// Empty error (this should never be returned to user, but is employed in case of error in control flow logic)
		// That way the only way nil is returned is if there is success
		err = fmt.Errorf("Assert: This error should be replaced by other errors or nil")
		if err = r.port.Flush(); err != nil {
			continue
		}
		crc = crcType(0)

		crc.update(address, 21)
		if _, err = r.port.Write([]uint8{address, 21}); err != nil {
			continue
		}

		// Reads up to 48 bytes
		for i := 0; i < 48; i++ {
			if _, err = io.ReadFull(r.port, buf[:1]); err == nil {

				//Use append to ensure that version is no longer than it
				//needs to be
				version = append(version, buf[0])
				crc.update(buf[0])

				if buf[0] == 0 {
					if _, err = io.ReadFull(r.port, buf[:]); err == nil {
						ccrc = uint16(buf[0]) << 8
						ccrc |= uint16(buf[1])

						if ccrc == uint16(crc) {
							return string(version), nil
						} else {
							err = fmt.Errorf("Mismatched checksum values")
							continue loop
						}
					} else {
						continue loop
					}
				}
			} else {
				continue loop
			}
		}
		err = fmt.Errorf("No end of version delimiter received")
	}
	return "", err
}

/*
 * Set encoder 1 counter to 0 (for quadrature encoders)
 * @param address {uint8}
 * @param val {int32}
 * @return {error}
 */
func (r *Roboclaw) SetEncM1(address uint8, val int32) error {
	return r.write_n(address, 22, setDWORDval(uint32(val))...)
}

/*
 * Set encoder 2 counter to 0 (for quadrature encoders)
 * @param address {uint8}
 * @param val {int32}
 * @return {error}
 */
func (r *Roboclaw) SetEncM2(address uint8, val int32) error {
	return r.write_n(address, 23, setDWORDval(uint32(val))...)
}

/*
 * Read the main battery voltage (connected to B+ and B- terminals)
 * @param address {uint8}
 * @return {uint16, error} voltage is returned in 10ths of a volt
 */
func (r *Roboclaw) ReadMainBatteryVoltage(address uint8) (uint16, error) {
	return r.read2(address, 24)
}

/*
 * Read the logic battery voltage (connected to LB+ and LB- terminals)
 * @param address {uint8}
 * @return {uint16, error} voltage is returned in 10ths of a volt
 */
func (r *Roboclaw) ReadLogicBatteryVoltage(address uint8) (uint16, error) {
	return r.read2(address, 25)
}

/*
 * Sets the minimum logic battery voltage
 * Volts are (Desired volts - 6) * 5
 * @param address {uint8}
 * @param voltage {uint8} valid data from 0 - 140 (6V to 34V)
 * @return {error}
 */
func (r *Roboclaw) SetMinVoltageLogicBattery(address uint8, voltage uint8) error {
	if voltage > 140 {
		return fmt.Errorf("Voltage above maximum of 140")
	}
	return r.write_n(address, 26, voltage)
}

/*
 * Sets the maximum logic battery voltage
 * Volts are Desired volts *  5.12
 * @param address {uint8}
 * @param voltage {uint8} valid data from 30 - 175 (6V to 34V)
 * @return {error}
 */
func (r *Roboclaw) SetMaxVoltageLogicBattery(address uint8, voltage uint8) error {
	if voltage > 175 {
		return fmt.Errorf("Voltage above maximum of 175")
	} else if voltage < 30 {
		return fmt.Errorf("Speed below minimum of 30")
	}
	return r.write_n(address, 27, voltage)
}

/*
 * Set velocity PID constant for motor 1
 * @param address {uint8}
 * @param kp_fp {float32} proportional constant. valid data from 0 to 65536
 * @param ki_fp {float32} integral constant. valid data from 0 to 65536
 * @param kd_fp {float32} derivative constant. valid data from 0 to 65536
 * @param qpps {uint32} speed of encoder when motor is at 100 %
 * @return {error}
 */
func (r *Roboclaw) SetM1VelocityPID(address uint8, kp_fp float32, ki_fp float32, kd_fp float32, qpps uint32) error {
	// Although the arguments to the motor controller is a uint32, the arguments
	// to the function are floats to match basic micro libraries
	// 65536 is the maximum value because it is multiplied by 65536 in the function,
	// the product of which is the maximum uint32
	if kp_fp > 65536 || kp_fp < 0 {
		return fmt.Errorf("Proportional contant outside of acceptable range (0 - 65536)")
	} else if ki_fp > 65536 || kp_fp < 0 {
		return fmt.Errorf("Integral contant outside of acceptable range (0 - 65536)")
	} else if kd_fp > 65536 || kp_fp < 0 {
		return fmt.Errorf("Derivative contant outside of acceptable range (0 - 65536)")
	}

	array := append(setDWORDval(uint32(kp_fp*65536)), setDWORDval(uint32(ki_fp*65536))...)
	array = append(array, setDWORDval(uint32(kd_fp*65536))...)
	array = append(array, setDWORDval(qpps)...)
	return r.write_n(address, 28, array...)
}

/*
 * Set velocity PID constant for motor 2
 * @param address {uint8}
 * @param kp_fp {float32} proportional constant. valid data from 0 to 65536
 * @param ki_fp {float32} integral constant. valid data from 0 to 65536
 * @param kd_fp {float32} derivative constant. valid data from 0 to 65536
 * @param qpps {uint32} speed of encoder when motor is at 100 %
 * @return {error}
 */
func (r *Roboclaw) SetM2VelocityPID(address uint8, kp_fp float32, ki_fp float32, kd_fp float32, qpps uint32) error {
	// Although the arguments to the motor controller is a uint32, the arguments
	// to the function are floats to match basic micro libraries
	// 65536 is the maximum value because it is multiplied by 65536 in the function,
	// the product of which is the maximum uint32
	if kp_fp > 65536 || kp_fp < 0 {
		return fmt.Errorf("Proportional contant outside of acceptable range (0 - 65536)")
	} else if ki_fp > 65536 || kp_fp < 0 {
		return fmt.Errorf("Integral contant outside of acceptable range (0 - 65536)")
	} else if kd_fp > 65536 || kp_fp < 0 {
		return fmt.Errorf("Derivative contant outside of acceptable range (0 - 65536)")
	}

	array := append(setDWORDval(uint32(kp_fp*65536)), setDWORDval(uint32(ki_fp*65536))...)
	array = append(array, setDWORDval(uint32(kd_fp*65536))...)
	array = append(array, setDWORDval(qpps)...)
	return r.write_n(address, 29, array...)
}

/*
 * Read raw speed for motor 1
 * Pulses counted in last 300th of a second, but returned as encoder counts per second
 * Direction is 0 for forward and 1 for backward
 * @param address {uint8}
 * @return {uint32, uint8, error} speed, direction, error
 */
func (r *Roboclaw) ReadISpeedM1(address uint8) (uint32, uint8, error) {
	return r.read4_1(address, 30)
}

/*
 * Read raw speed for motor 2
 * Pulses counted in last 300th of a second, but returned as encoder counts per second
 * Direction is 0 for forward and 1 for backward
 * @param address {uint8}
 * @return {uint32, uint8, error} speed, direction, error
 */
func (r *Roboclaw) ReadISpeedM2(address uint8) (uint32, uint8, error) {
	return r.read4_1(address, 31)
}

/*
 * Drive motor 1 in duty cycle mode
 * Values represent +/- 100% duty
 * @param address {uint8}
 * @param duty {int16}
 * @return {error}
 */
func (r *Roboclaw) DutyM1(address uint8, duty int16) error {
	return r.write_n(address, 32, setWORDval(uint16(duty))...)
}

/*
 * Drive motor 2 in duty cycle mode
 * Values represent +/- 100% duty
 * @param address {uint8}
 * @param duty {int16}
 * @return {error}
 */
func (r *Roboclaw) DutyM2(address uint8, duty int16) error {
	return r.write_n(address, 33, setWORDval(uint16(duty))...)
}

/*
 * Drive motor 1 and 2 in duty cycle mode
 * Values represent +/- 100% duty
 * @param address {uint8}
 * @param duty1 {int16}
 * @param duty2 {int16}
 * @return {error}
 */
func (r *Roboclaw) DutyM1M2(address uint8, duty1 int16, duty2 int16) error {
	return r.write_n(address, 34, append(setWORDval(uint16(duty1)), setWORDval(uint16(duty2))...)...)
}

/*
 * Drive motor 1 at given speed. Sign indicates direction.
 * @param address {uint8}
 * @param speed {int32}
 * @return {error}
 */
func (r *Roboclaw) SpeedM1(address uint8, speed int32) error {
	return r.write_n(address, 35, setDWORDval(uint32(speed))...)
}

/*
 * Drive motor 2 at given speed. Sign indicates direction.
 * @param address {uint8}
 * @param speed {int32}
 * @return {error}
 */
func (r *Roboclaw) SpeedM2(address uint8, speed int32) error {
	return r.write_n(address, 36, setDWORDval(uint32(speed))...)
}

/*
 * Drive motors 1 and 2 at the given speeds. Sign indicates direction.
 * @param address {uint8}
 * @param speed1 {int32}
 * @param speed2 {int32}
 * @return {error}
 */
func (r *Roboclaw) SpeedM1M2(address uint8, speed1 int32, speed2 int32) error {
	return r.write_n(address, 37, append(setDWORDval(uint32(speed1)), setDWORDval(uint32(speed2))...)...)
}

/*
 * Drive motor 1 with speed and acceleration
 * @param address {uint8}
 * @param accel {uint32} acceleration is unsigned
 * @param speed {int32}
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelM1(address uint8, accel uint32, speed int32) error {
	return r.write_n(address, 38, append(setDWORDval(accel), setDWORDval(uint32(speed))...)...)
}

/*
 * Drive motor 2 with speed and acceleration
 * @param address {uint8}
 * @param accel {uint32} acceleration is unsigned
 * @param speed {int32}
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelM2(address uint8, accel uint32, speed int32) error {
	return r.write_n(address, 39, append(setDWORDval(accel), setDWORDval(uint32(speed))...)...)
}

/*
 * Drive motors 1 and 2 with speed and acceleration
 * @param address {uint8}
 * @param accel {uint32} acceleration is unsigned
 * @param speed1 {int32}
 * @param speed2 {int32}
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelM1M2(address uint8, accel uint32, speed1 int32, speed2 int32) error {
	array := append(setDWORDval(accel), setDWORDval(uint32(speed1))...)
	return r.write_n(address, 40, append(array, setDWORDval(uint32(speed2))...)...)
}

/*
 * Drive motors 1 with speed to distance
 * @param address {uint8}
 * @param speed {int32}
 * @param distance {uint32} distance is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedDistanceM1(address uint8, speed int32, distance uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(uint32(speed)), setDWORDval(distance)...)
	return r.write_n(address, 41, append(array, flag)...)
}

/*
 * Drive motors 2 with speed to distance
 * @param address {uint8}
 * @param speed {int32}
 * @param distance {uint32} distance is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedDistanceM2(address uint8, speed int32, distance uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(uint32(speed)), setDWORDval(distance)...)
	return r.write_n(address, 42, append(array, flag)...)
}

/*
 * Drive motors 1 and 2 with speed to distance
 * @param address {uint8}
 * @param speed1 {int32}
 * @param distance1 {uint32} distance is unsigned
 * @param speed2 {int32}
 * @param distance2 {uint32} distance is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedDistanceM1M2(address uint8, speed1 int32, distance1 uint32, speed2 int32, distance2 uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(uint32(speed1)), setDWORDval(distance1)...)
	array = append(array, setDWORDval(uint32(speed2))...)
	array = append(array, setDWORDval(distance2)...)
	return r.write_n(address, 43, append(array, flag)...)
}

/*
 * Drive motor 1 with acceleration and speed to distance
 * @param address {uint8}
 * @param accel {uint32}	acceleration is unsigned
 * @param speed {int32}
 * @param distance {uint32} distance is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelDistanceM1(address uint8, accel uint32, speed int32, distance uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(accel), setDWORDval(uint32(speed))...)
	array = append(array, setDWORDval(distance)...)
	return r.write_n(address, 44, append(array, flag)...)
}

/*
 * Drive motor 2 with acceleration and speed to distance
 * @param address {uint8}
 * @param accel {uint32}	acceleration is unsigned
 * @param speed {int32}
 * @param distance {uint32} distance is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelDistanceM2(address uint8, accel uint32, speed int32, distance uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(accel), setDWORDval(uint32(speed))...)
	array = append(array, setDWORDval(distance)...)
	return r.write_n(address, 45, append(array, flag)...)
}

/*
 * Drive motors 1 and 2 with acceleration and speed to distance
 * @param address {uint8}
 * @param accel {uint32}	acceleration is unsigned
 * @param speed1 {int32}
 * @param distance1 {uint32} distance is unsigned
 * @param speed2 {int32}
 * @param distance2 {uint32} distance is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelDistanceM1M2(address uint8, accel uint32, speed1 int32, distance1 uint32, speed2 int32, distance2 uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(accel), setDWORDval(uint32(speed1))...)
	array = append(array, setDWORDval(distance1)...)
	array = append(array, setDWORDval(uint32(speed2))...)
	array = append(array, setDWORDval(distance2)...)
	return r.write_n(address, 46, append(array, flag)...)
}

/*
 * Read buffer lengths
 * Maximum length is 64,
 * A value of 128 means that the buffer is empty and all commands are finished
 * A value of 0 means that the last command is executing
 * @param address {uint8}
 * @return {uint8, uint8, error} buffer 1 length, buffer 2 length, error
 */
func (r *Roboclaw) ReadBuffers(address uint8) (uint8, uint8, error) {
	value, valid := r.read2(address, 47)
	return uint8(value >> 8), uint8(value), valid
}

/*
 * Read pwm values
 * PWM values are +/- 32767
 * Divide by 327.67 to get duty cycle percent
 * @param address {uint8}
 * @return {int16, int16, error} m1 pwm, m2 pwm, error
 */
func (r *Roboclaw) ReadPWMs(address uint8) (int16, int16, error) {
	value, valid := r.read4(address, 48)
	return int16(value >> 16), int16(value), valid
}

/*
 * Read currents
 * Values in 10 mA increments
 * Divide by 100 to get A
 * @param address {uint8}
 * @return {int16, int16, error} m1 current, m2 current, error
 */
func (r *Roboclaw) ReadCurrents(address uint8) (int16, int16, error) {
	value, valid := r.read4(address, 49)
	return int16(value >> 16), int16(value), valid
}

/*
 * Drive motors 1 and 2 with acceleration and speed
 * @param address {uint8}
 * @param accel1 {uint32}	acceleration is unsigned
 * @param speed1 {int32}
 * @param accel2 {uint32}	acceleration is unsigned
 * @param speed2 {int32}
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelM1M2_2(address uint8, accel1 uint32, speed1 int32, accel2 uint32, speed2 int32) error {
	array := append(setDWORDval(accel1), setDWORDval(uint32(speed1))...)
	array = append(array, setDWORDval(accel2)...)
	array = append(array, setDWORDval(uint32(speed2))...)
	return r.write_n(address, 50, array...)
}

/*
 * Drive motors 1 and 2 with acceleration and speed
 * @param address {uint8}
 * @param accel1 {uint32}	acceleration is unsigned
 * @param speed1 {int32}
 * @param distance1 {uint32}	distance is unsigned
 * @param accel2 {uint32}	acceleration is unsigned
 * @param speed2 {int32}
 * @param distance2 {uint32}	distance is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelDistanceM1M2_2(address uint8, accel1 uint32, speed1 int32, distance1 uint32, accel2 uint32, speed2 int32, distance2 uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(accel1), setDWORDval(uint32(speed1))...)
	array = append(array, setDWORDval(distance1)...)
	array = append(array, setDWORDval(accel2)...)
	array = append(array, setDWORDval(uint32(speed2))...)
	array = append(array, setDWORDval(distance2)...)
	return r.write_n(address, 51, append(array, flag)...)
}

/*
 * Drive motor 1 with duty and acceleration
 * @param address {uint8}
 * @param duty {int16}
 * @param accel {uint32} the acceleration. Valid data from 0 to 655359 (-100% to 100% in 100ms)
 * @return {error}
 */
func (r *Roboclaw) DutyAccelM1(address uint8, duty int16, accel uint32) error {
	if accel > 655359 {
		return fmt.Errorf("Acceleration above maximum value of 655359")
	}
	return r.write_n(address, 52, append(setWORDval(uint16(duty)), setDWORDval(accel)...)...)
}

/*
 * Drive motor 2 with duty and acceleration
 * @param address {uint8}
 * @param duty {int16}
 * @param accel {uint32} the acceleration. Valid data from 0 to 655359 (-100% to 100% in 100ms)
 * @return {error}
 */
func (r *Roboclaw) DutyAccelM2(address uint8, duty int16, accel uint32) error {
	if accel > 655359 {
		return fmt.Errorf("Acceleration above maximum value of 655359")
	}
	return r.write_n(address, 53, append(setWORDval(uint16(duty)), setDWORDval(accel)...)...)
}

/*
 * Drive motors 1 and 2 with duty and acceleration
 * @param address {uint8}
 * @param duty1 {int16}
 * @param accel1 {uint32} the acceleration. Valid data from 0 to 655359 (-100% to 100% in 100ms)
 * @param duty2 {int16}
 * @param accel2 {uint32} the acceleration. Valid data from 0 to 655359 (-100% to 100% in 100ms)
 * @return {error}
 */
func (r *Roboclaw) DutyAccelM1M2(address uint8, duty1 int16, accel1 uint32, duty2 int16, accel2 uint32) error {
	if accel1 > 655359 {
		return fmt.Errorf("Acceleration for m1 above maximum value of 655359")
	}
	if accel2 > 655359 {
		return fmt.Errorf("Acceleration for m2 above maximum value of 655359")
	}
	array := append(setWORDval(uint16(duty1)), setDWORDval(accel1)...)
	array = append(array, setWORDval(uint16(duty2))...)
	array = append(array, setDWORDval(accel2)...)
	return r.write_n(address, 54, array...)
}

/*
 * Read motor 1 PID and QPPS
 * @param address {uint8}
 * @return {float32, float32, float32, uint32, error} proportional, integral, derivative, qpps, error
 */
func (r *Roboclaw) ReadM1VelocityPID(address uint8) (float32, float32, float32, uint32, error) {
	var Kp, Ki, Kd, qpps uint32
	err := r.read_n(address, 55, &Kp, &Ki, &Kd, &qpps)
	return float32(Kp / 65536), float32(Ki / 65536), float32(Kd / 65536), qpps, err
}

/*
 * Read motor 2 PID and QPPS
 * @param address {uint8}
 * @return {float32, float32, float32, uint32, error} proportional, integral, derivative, qpps, error
 */
func (r *Roboclaw) ReadM2VelocityPID(address uint8) (float32, float32, float32, uint32, error) {
	var Kp, Ki, Kd, qpps uint32
	err := r.read_n(address, 56, &Kp, &Ki, &Kd, &qpps)
	return float32(Kp / 65536), float32(Ki / 65536), float32(Kd / 65536), qpps, err
}

/*
 * Sets the minimum and maximum main battery voltage
 * @param address {uint8}
 * @param min {uint16} Values in 10th of a volt
 * @param max {uint16} Values in 10th of a volt
 * @return {error}
 */
func (r *Roboclaw) SetMainVoltages(address uint8, min uint16, max uint16) error {
	return r.write_n(address, 57, append(setWORDval(min), setWORDval(max)...)...)
}

/*
 * Sets the minimum and maximum logic battery voltage
 * @param address {uint8}
 * @param min {uint16} Values in 10th of a volt
 * @param max {uint16} Values in 10th of a volt
 * @return {error}
 */
func (r *Roboclaw) SetLogicVoltages(address uint8, min uint16, max uint16) error {
	return r.write_n(address, 58, append(setWORDval(min), setWORDval(max)...)...)
}

/*
 * Reads min and max main battery voltage
 * Voltages are in mV
 * @param address {uint8}
 * @return {uint16, uint16, error} min, max, error
 */
func (r *Roboclaw) ReadMinMaxMainVoltages(address uint8) (uint16, uint16, error) {
	value, valid := r.read4(address, 59)
	return uint16(value >> 16), uint16(value), valid
}

/*
 * Reads min and max logic battery voltage
 * Voltages are in mV
 * @param address {uint8}
 * @return {uint16, uint16, error} min, max, error
 */
func (r *Roboclaw) ReadMinMaxLogicVoltages(address uint8) (uint16, uint16, error) {
	value, valid := r.read4(address, 60)
	return uint16(value >> 16), uint16(value), valid
}

/*
 * Set position PID constants for motor 1
 * @param address {uint8}
 * @param kp_fp {float32} proportional constant. valid data from 0 to 4194304
 * @param ki_fp {float32} integral constant. valid data from 0 to 4194304
 * @param kd_fp {float32} derivative constant. valid data from 0 to 4194304
 * @param kiMax {uint32} maximum integral windup
 * @param deadzone {uint32} encoder counts deadzone
 * @param min {uint32} minimum position
 * @param max {uint32} maximum position
 * @return {error}
 */
func (r *Roboclaw) SetM1PositionPID(address uint8, kp_fp float32, ki_fp float32, kd_fp float32, kiMax uint32, deadzone uint32, min uint32, max uint32) error {
	// Although the arguments to the motor controller is a uint32, the arguments
	// to the function are floats to match basic micro libraries
	// 4194304 is the maximum value because it is multiplied by 1024 in the function,
	// the product of which is the maximum uint32
	if kp_fp > 4194304 || kp_fp < 0 {
		return fmt.Errorf("Proportional contant outside of acceptable range (0 - 4194304)")
	} else if ki_fp > 4194304 || kp_fp < 0 {
		return fmt.Errorf("Integral contant outside of acceptable range (0 - 4194304)")
	} else if kd_fp > 4194304 || kp_fp < 0 {
		return fmt.Errorf("Derivative contant outside of acceptable range (0 - 4194304)")
	}

	array := append(setDWORDval(uint32(kp_fp*1024)), setDWORDval(uint32(ki_fp*1024))...)
	array = append(array, setDWORDval(uint32(kd_fp*1024))...)
	array = append(array, setDWORDval(kiMax)...)
	array = append(array, setDWORDval(deadzone)...)
	array = append(array, setDWORDval(min)...)
	array = append(array, setDWORDval(max)...)
	return r.write_n(address, 61, array...)
}

/*
 * Set position PID constants for motor 2
 * @param address {uint8}
 * @param kp_fp {float32} proportional constant. valid data from 0 to 4194304
 * @param ki_fp {float32} integral constant. valid data from 0 to 4194304
 * @param kd_fp {float32} derivative constant. valid data from 0 to 4194304
 * @param kiMax {uint32} maximum integral windup
 * @param deadzone {uint32} encoder counts deadzone
 * @param min {uint32} minimum position
 * @param max {uint32} maximum position
 * @return {error}
 */
func (r *Roboclaw) SetM2PositionPID(address uint8, kp_fp float32, ki_fp float32, kd_fp float32, kiMax uint32, deadzone uint32, min uint32, max uint32) error {
	// Although the arguments to the motor controller is a uint32, the arguments
	// to the function are floats to match basic micro libraries
	// 4194304 is the maximum value because it is multiplied by 1024 in the function,
	// the product of which is the maximum uint32
	if kp_fp > 4194304 || kp_fp < 0 {
		return fmt.Errorf("Proportional contant outside of acceptable range (0 - 4194304)")
	} else if ki_fp > 4194304 || kp_fp < 0 {
		return fmt.Errorf("Integral contant outside of acceptable range (0 - 4194304)")
	} else if kd_fp > 4194304 || kp_fp < 0 {
		return fmt.Errorf("Derivative contant outside of acceptable range (0 - 4194304)")
	}

	array := append(setDWORDval(uint32(kp_fp*1024)), setDWORDval(uint32(ki_fp*1024))...)
	array = append(array, setDWORDval(uint32(kd_fp*1024))...)
	array = append(array, setDWORDval(kiMax)...)
	array = append(array, setDWORDval(deadzone)...)
	array = append(array, setDWORDval(min)...)
	array = append(array, setDWORDval(max)...)
	return r.write_n(address, 62, array...)
}

/*
 * Read position PID constants for motor 2
 * @param address {uint8}
 * @return {float32, float32, float32, uint32, uint32, uint32, uint32, error} proportional, integral, derivative, max integral windup, min position, max position, error
 */
func (r *Roboclaw) ReadM1PositionPID(address uint8) (float32, float32, float32, uint32, uint32, uint32, uint32, error) {
	var Kp, Ki, Kd, KiMax, DeadZone, Min, Max uint32
	err := r.read_n(address, 63, &Kp, &Ki, &Kd, &KiMax, &DeadZone, &Min, &Max)
	return float32(Kp / 1024), float32(Ki / 1024), float32(Kd / 1024), KiMax, DeadZone, Min, Max, err
}

/*
 * Read position PID constants for motor 2
 * @param address {uint8}
 * @return {float32, float32, float32, uint32, uint32, uint32, uint32, error} proportional, integral, derivative, max integral windup, min position, max position, error
 */
func (r *Roboclaw) ReadM2PositionPID(address uint8) (float32, float32, float32, uint32, uint32, uint32, uint32, error) {
	var Kp, Ki, Kd, KiMax, DeadZone, Min, Max uint32
	err := r.read_n(address, 64, &Kp, &Ki, &Kd, &KiMax, &DeadZone, &Min, &Max)
	return float32(Kp / 1024), float32(Ki / 1024), float32(Kd / 1024), KiMax, DeadZone, Min, Max, err
}

/*
 * Drive motor 1  with acceleration and speed
 * @param address {uint8}
 * @param accel {uint32}	acceleration is unsigned
 * @param speed {int32}
 * @param deccel {uint32}	decceleration is unsigned
 * @param position {uint32}	position is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelDeccelPositionM1(address uint8, accel uint32, speed int32, deccel uint32, position uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(accel), setDWORDval(uint32(speed))...)
	array = append(array, setDWORDval(deccel)...)
	array = append(array, setDWORDval(position)...)
	return r.write_n(address, 65, append(array, flag)...)
}

/*
 * Drive motor 2  with acceleration and speed
 * @param address {uint8}
 * @param accel {uint32}	acceleration is unsigned
 * @param speed {int32}
 * @param deccel {uint32}	decceleration is unsigned
 * @param position {uint32}	position is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelDeccelPositionM2(address uint8, accel uint32, speed int32, deccel uint32, position uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(accel), setDWORDval(uint32(speed))...)
	array = append(array, setDWORDval(deccel)...)
	array = append(array, setDWORDval(position)...)
	return r.write_n(address, 66, append(array, flag)...)
}

/*
 * Drive motors 1 and 2 with acceleration and speed
 * @param address {uint8}
 * @param accel1 {uint32}	acceleration is unsigned
 * @param speed1 {int32}
 * @param deccel1 {uint32}	decceleration is unsigned
 * @param position1 {uint32}	position is unsigned
 * @param accel2 {uint32}	acceleration is unsigned
 * @param speed2 {int32}
 * @param deccel2 {uint32}	decceleration is unsigned
 * @param position2 {uint32}	position is unsigned
 * @param buffer {bool} true to override the previous command, false to buffer
 * @return {error}
 */
func (r *Roboclaw) SpeedAccelDeccelPositionM1M2(address uint8, accel1 uint32, speed1 int32, deccel1 uint32, position1 uint32, accel2 uint32, speed2 int32, deccel2 uint32, position2 uint32, buffer bool) error {
	var flag uint8
	if buffer {
		flag = 1
	} else {
		flag = 0
	}
	array := append(setDWORDval(accel1), setDWORDval(uint32(speed1))...)
	array = append(array, setDWORDval(deccel1)...)
	array = append(array, setDWORDval(position1)...)
	array = append(array, setDWORDval(accel2)...)
	array = append(array, setDWORDval(uint32(speed2))...)
	array = append(array, setDWORDval(deccel2)...)
	array = append(array, setDWORDval(position2)...)
	return r.write_n(address, 67, append(array, flag)...)
}

/*
 * Sets the default accelaration for duty cycle commands with motor 1
 * @param address {uint8}
 * @param accel {uint32}
 * @return {error}
 */
func (r *Roboclaw) SetM1DefaultAccel(address uint8, accel uint32) error {
	return r.write_n(address, 68, setDWORDval(accel)...)
}

/*
 * Sets the default accelaration for duty cycle commands with motor 2
 * @param address {uint8}
 * @param accel {uint32}
 * @return {error}
 */
func (r *Roboclaw) SetM2DefaultAccel(address uint8, accel uint32) error {
	return r.write_n(address, 69, setDWORDval(accel)...)
}

/*
 * Sets the pin modes
 * @param address {uint8}
 * @param s3mode {uint8} valid data 0 - 4
 * @param s4mode {uint8} valid data 0 - 4
 * @param s5mode {uint8} valid data 0 - 4
 * @return {error}
 */
func (r *Roboclaw) SetPinFunctions(address uint8, s3mode uint8, s4mode uint8, s5mode uint8) error {
	if s3mode > 4 {
		return fmt.Errorf("S3 mode above 4")
	} else if s4mode > 4 {
		return fmt.Errorf("S4 mode above 4")
	} else if s5mode > 4 {
		return fmt.Errorf("S5 mode above 4")
	}
	return r.write_n(address, 74, s3mode, s4mode, s5mode)
}

/*
 * Read the pin modes
 * @param address {uint8}
 * @return {uint8, uint8, uint8, error} s3 mode, s4 mode, s5 mode, error
 */
func (r *Roboclaw) GetPinFunctions(address uint8) (uint8, uint8, uint8, error) {

	var (
		crc  crcType
		ccrc uint16
		buf  [5]uint8
		err  error
	)

	for trys := r.retries; trys > 0; trys-- {
		// Empty error (this should never be returned to user, but is employed in case of error in control flow logic)
		// That way the only way nil is returned is if there is success
		err = fmt.Errorf("Assert: This error should be replaced by other errors or nil")
		if err = r.port.Flush(); err != nil {
			continue
		}
		crc = crcType(0)

		crc.update(address, 75)
		if _, err = r.port.Write([]uint8{address, 75}); err != nil {
			continue
		}

		if _, err = io.ReadFull(r.port, buf[:]); err == nil {
			crc.update(buf[0:3]...)

			ccrc = uint16(buf[3]) << 8
			ccrc |= uint16(buf[4])

			if ccrc == uint16(crc) {
				return buf[0], buf[1], buf[2], nil
			} else {
				err = fmt.Errorf("Mismatched checksum values")
			}
		}
	}
	return 0, 0, 0, err
}

/*
 * Set deadband for RC/Analog control
 * RC/Analog mode control in 10th of a percent
 * @param address {uint8}
 * @param Min {uint8} valid data 0 - 250 (0 to 25%)
 * @param Max {uint8} valid data 0 - 250 (0 to 25%)
 * @return {error}
 */
func (r *Roboclaw) SetDeadBand(address uint8, Min uint8, Max uint8) error {
	if Min > 250 {
		return fmt.Errorf("Min deadband above 250")
	} else if Max > 250 {
		return fmt.Errorf("Max deadband above 250")
	}
	return r.write_n(address, 76, Min, Max)
}

/*
 * Read deadband for RC/Analog control
 * RC/Analog mode control in 10th of a percent
 * @param address {uint8}
 * @return {uint8, uint8, error} min, max, error
 */
func (r *Roboclaw) GetDeadBand(address uint8) (uint8, uint8, error) {
	value, valid := r.read2(address, 77)
	return uint8(value >> 8), uint8(value), valid
}

/*
 * Read encoder counters
 * @param address {uint8}
 * @return {uint32, uint32, error} encoder 1 counter, encoder 2 counter, error
 */
func (r *Roboclaw) ReadEncoders(address uint8) (uint32, uint32, error) {
	var enc1, enc2 uint32
	err := r.read_n(address, 78, &enc1, &enc2)
	return enc1, enc2, err
}

/*
 * Read instantaneous speed
 * Speed is in counter per second measured over last 300th of a second
 * @param address {uint8}
 * @return {uint32, uint32, error} speed encoder 1, speed encoder 2 , error
 */
func (r *Roboclaw) ReadISpeeds(address uint8) (uint32, uint32, error) {
	var ispeed1, ispeed2 uint32
	err := r.read_n(address, 79, &ispeed1, &ispeed2)
	return ispeed1, ispeed2, err
}

/*
 * Restore default values
 * @param address {uint8}
 * @return {error}
 */
func (r *Roboclaw) RestoreDefaults(address uint8) error {
	return r.write_n(address, 80)
}

/*
 * Read the default acceleration values
 * @param address {uint8}
 * @return {uint32, uint32, error} acceleration m1, acceleration m2, error
 */
func (r *Roboclaw) ReadDefaultAcceleration(address uint8) (uint32, uint32, error) {
	return r.read4_4(address, 81)
}

/*
 * Restore board temperature
 * Value is in 10th of a degree
 * @param address {uint8}
 * @return {error} temp, error
 */
func (r *Roboclaw) ReadTemp(address uint8) (uint16, error) {
	return r.read2(address, 82)
}

/*
 * Read second board temperature (only on certain devices)
 * Value is in 10th of a degree
 * @param address {uint8}
 * @return {uint16, error} temp, error
 */
func (r *Roboclaw) ReadTemp2(address uint8) (uint16, error) {
	return r.read2(address, 83)
}

/*
 * Read any statuses from the roboclaw
 * The reference manual says that the status is a 16 bit int,
 * but recently the roboclaws have been returning a 32 bit int
 * This probably occurred due to a firmware update, though
 * I have not yet found where it is documented
 * @param address {uint8}
 * @return {uint16, error} address, error
 */
func (r *Roboclaw) ReadError(address uint8) (uint32, error) {
	return r.read4(address, 90)
}

/*
 * Read encoder modes
 * @param address {uint8}
 * @return {uint8, uint8, error} encode 1 mode, encoder 2 mode, error
 */
func (r *Roboclaw) ReadEncoderModes(address uint8) (uint8, uint8, error) {
	value, valid := r.read2(address, 91)
	return uint8(value >> 8), uint8(value), valid
}

/*
 * Set motor 1 encoder mode
 * @param address {uint8}
 * @param mode {uint8}
 * @return {error}
 */
func (r *Roboclaw) SetM1EncoderMode(address uint8, mode uint8) error {
	// The only valid bits are bit 0 and bit 7
	// All others are non-applicable
	if 0 != (mode & 0x7E) {
		return fmt.Errorf("Encoder mode has unused bits set")
	}
	return r.write_n(address, 92, mode)
}

/*
 * Set motor 2 encoder mode
 * @param address {uint8}
 * @param mode {uint8}
 * @return {error}
 */
func (r *Roboclaw) SetM2EncoderMode(address uint8, mode uint8) error {
	// The only valid bits are bit 0 and bit 7
	// All others are non-applicable
	if 0 != (mode & 0x7E) {
		return fmt.Errorf("Encoder mode has unused bits set")
	}
	return r.write_n(address, 93, mode)
}

/*
 * Write settings to non-volatile memory
 * @param address {uint8}
 * @return {error}
 */
func (r *Roboclaw) WriteNVM(address uint8) error {
	// The software libraries to not match the user manual description
	// The compiled code corresponds to the user manual
	return r.write_n(address, 94)

	// The following code matches that found in the arduino and python
	// software libraries provided by basic micro
	// return r.write_n(address, WRITENVM, setDWORDval(0xE22EAB7A)...)
}

/*
 * Read settings from non-volatile memory
 * @param address {uint8}
 * @return {uint8, uint8, error} encoder 1 mode, encoder 2 mode, error
 */
func (r *Roboclaw) ReadNVM(address uint8) (uint8, uint8, error) {
	// The software libraries to not match the user manual description
	// The compiled code corresponds to the user manual
	value, valid := r.read2(address, 95)
	return uint8(value >> 8), uint8(value), valid

	// The following code matches that found in the arduino and python
	// software libraries provided by basic micro
	//return r.write_n(address, READNVM)
}

/*
 * Set config settings
 * @param address {uint8}
 * @param config {uint16}
 * @return {error}
 */
func (r *Roboclaw) SetConfig(address uint8, config uint16) error {
	return r.write_n(address, 98, setWORDval(config)...)
}

/*
 * Read config settings
 * @param address {uint8}
 * @return {uint16, error} settings, error
 */
func (r *Roboclaw) GetConfig(address uint8) (uint16, error) {
	return r.read2(address, 99)
}

/*
 * Set CTRL mode (only certain models)
 * NOTE: This function is not properly documented in the user manual
 * Based on command 101 I have tried to infer the correct command codes
 * @param address {uint8}
 * @param ctrl1 {uint8}
 * @param ctrl2 {uint8}
 * @return {error}
 */
func (r *Roboclaw) SetCTRLMode(address uint8, ctrl1 uint8, ctrl2 uint8) error {
	if ctrl1 > 3 {
		return fmt.Errorf("Ctrl1 mode value above 3")
	} else if ctrl2 > 3 {
		return fmt.Errorf("Ctrl2 mode value above 3")
	}
	return r.write_n(address, 100, ctrl1, ctrl2)
}

/*
 * Read CTRL modes (only certain models)
 * @param address {uint8}
 * @return {uint8, uint8, error} ctrl 1 mode, ctrl 2 mode, error
 */
func (r *Roboclaw) ReadCTRLMode(address uint8) (uint8, uint8, error) {
	value, valid := r.read2(address, 101)
	return uint8(value >> 8), uint8(value), valid
}

/*
 * Set CTRL1 output value (only certain models)
 * @param address {uint8}
 * @param value {uint16}
 * @return {error}
 */
func (r *Roboclaw) SetCTRL1(address uint8, value uint16) error {
	return r.write_n(address, 102, setWORDval(value)...)
}

/*
 * Set CTRL2 output value (only certain models)
 * @param address {uint8}
 * @param value {uint16}
 * @return {error}
 */
func (r *Roboclaw) SetCTRL2(address uint8, value uint16) error {
	return r.write_n(address, 103, setWORDval(value)...)
}

/*
 * Read CTRL value settings
 * @param address {uint8}
 * @return {uint16, uint16, error} ctrl1, ctrl2, error
 */
func (r *Roboclaw) ReadCTRL12(address uint8) (uint16, uint16, error) {
	value, valid := r.read4(address, 104)
	return uint16(value >> 16), uint16(value), valid
}

/*
 * Set m1 max current
 * @param address {uint8}
 * @param max {uint32} the maximum current in 10 mA units
 * @return {error}
 */
func (r *Roboclaw) SetM1MaxCurrent(address uint8, max uint32) error {
	return r.write_n(address, 133, append(setDWORDval(max), setDWORDval(0)...)...)
}

/*
 * Set m2 max current
 * @param address {uint8}
 * @param max {uint32} the maximum current in 10 mA units
 * @return {error}
 */
func (r *Roboclaw) SetM2MaxCurrent(address uint8, max uint32) error {
	return r.write_n(address, 134, append(setDWORDval(max), setDWORDval(0)...)...)
}

/*
 * Read m1 max current
 * Current in in 10 mA units
 * @param address {uint8}
 * @return {uint32, error} max current, error
 */
func (r *Roboclaw) ReadM1MaxCurrent(address uint8) (uint32, error) {
	var tmax, dummy uint32
	err := r.read_n(address, 135, &tmax, &dummy)
	return tmax, err
}

/*
 * Read m2 max current
 * Current in in 10 mA units
 * @param address {uint8}
 * @return {uint32, error} max current, error
 */
func (r *Roboclaw) ReadM2MaxCurrent(address uint8) (uint32, error) {
	var tmax, dummy uint32
	err := r.read_n(address, 136, &tmax, &dummy)
	return tmax, err
}

/*
 * Set PWM mode
 * @param address {uint8}
 * @param mode {uint8} valid values are 0 and 1
 * @return {error}
 */
func (r *Roboclaw) SetPWMMode(address uint8, mode uint8) error {
	if mode != 0 && mode != 1 {
		return fmt.Errorf("PWM Mode is value other than 0 or 1")
	}
	return r.write_n(address, 148, mode)
}

/*
 * Read PWM mode
 * @param address {uint8}
 * @return {uint8, error} mode, error
 */
func (r *Roboclaw) GetPWMMode(address uint8) (uint8, error) {
	value, valid := r.read1(address, 149)
	return value, valid
}
