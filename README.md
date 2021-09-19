Makerspace Tag
==============

_Super quick and incomplete hack. Nothing to see here. Move along._

Tagging in and out makerspace users. Stores the timestamp and RFID card
serial in a log file.

In a second CSV file, RFID card serials and names and tool permissions
are stored.

Currently paths are pretty hardcoded. The BASEDIR is `/home/pi` for instance

  * sounds: `${BASEDIR}/accept.wav`, `${BASEDIR}/attention.wav`
  * templates: `${BASEDIR}/template/tagin.html`
  * logfile: `${BASEDIR}/tag-log/`. Within that, dated logfiles such
    as `log-2021-09-19.csv`
  * users file: `${BASEDIR}/tag-users.csv`


To show an feedback light, uses https://github.com/hzeller/microorb

To compile we need golang and libnfc
```
sudo aptitude install golang libnfc-dev
```

nfc version needs to be at least 1.8.0
https://github.com/nfc-tools/libnfc/releases

## Building

First time
```
go mod download github.com/clausecker/nfc/v2
make
```

Then afterwards, just
```
make
```

should do it.

## Running
```
./makerspace-tag
```

### /etc/systemd/system/microorb.service
```ini
[Unit]
Description=Microorb
StartLimitIntervalSec=0

[Service]
Type=simple
Restart=always
RestartSec=1
User=pi
ExecStart=/usr/local/bin/microorb -P 9999

[Install]
WantedBy=multi-user.target
```


### /etc/systemd/system/makerspace_tag.service
```ini
[Unit]
Description=Makerspace Tag
StartLimitIntervalSec=0

[Service]
Type=simple
Restart=always
RestartSec=1
User=pi
ExecStart=/home/pi/bin/run-makerspace-tag.sh

[Install]
WantedBy=multi-user.target
```

With `run-makerspace-tag.sh`
```bash
#!/bin/bash
cd /home/pi/src/tag-in-out/
./makerspace_tag

```

To make things complete, run chromium in full screen, connecting to the URL
of the makertag.

Something like this ~/bin/kiosk.sh

```bash
#!/bin/bash

# Don't have screen power management kick in.
xset s noblank
xset s off
xset -dpms

/usr/bin/chromium-browser --noerrdialogs --disable-infobars --kiosk http://localhost:2000/ &
```

Then, maybe put in some autostart, e.g.
`/etc/xdg/autostart/kiosk.desktop`
```ini
[Desktop Entry]
Exec=/home/pi/bin/kiosk.sh`

```