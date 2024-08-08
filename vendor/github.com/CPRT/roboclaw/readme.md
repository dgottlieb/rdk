# roboclaw
Library for the RoboClaws motor controller from Ion Motion Control written in Go.

This library was based on the Roboclaw 2x7A sample library for the Arduino 
found at http://www.basicmicro.com/downloads.

# example

Initialize the roboclaw
```
	var robo *roboclaw.Roboclaw
	var err error

	robo, err = roboclaw.Init(&roboclaw.Config{Name: "/dev/ttyAMA0", Baud: 19200, Retries: 3})
```

Read the roboclaw version on a roboclaw assigned address 128
```
	var version string

	version, err = robo.ReadVersion(128)
```

Drive motor 1 forward at maximum duty cycle
```
	err = robo.DutyM1(128, 32767)
```