import datetime
from json import JSONDecoder
from tempfile import NamedTemporaryFile
from pprint import pprint
from collections import defaultdict
import subprocess
import sys

class GnuPlotWriter(object):
    def __init__(self):
        self.datfiles = defaultdict(lambda: NamedTemporaryFile('w', delete=True))
        self.png_filename = None

    def play(self, obj):
        pprint(obj)
        start = obj["Time"]
        for key, value in obj["Data"].items():
            self.play_val(start, key, value)

    def play_val(self, time, keyns, inp_value):
        if isinstance(inp_value, dict):
            for key, value in inp_value.items():
                self.play_val(time, f'{keyns}.{key}', value)
        else:
            if inp_value == True:
                inp_value = 1
            elif inp_value == False:
                inp_value = 0

            self.datfiles[keyns].write(f'{time} {inp_value}\n')

    def get_dat(self, key):
        if key in self.datfiles:
            return self.datfiles[key]

        self.datfiles[key] = NamedTemporaryFile()

    def plot(self, render_options):
        #outfile = NamedTemporaryFile(mode='w')
        outfile = open('/home/dgottlieb/viam/rdk/ftdc/plot_py.gpl', 'w')
        if not self.png_filename:
            self.png_filename = '/home/dgottlieb/viam/rdk/ftdc/plot_py.png'

        def writeln(line):
            outfile.write(f'{line}\n')

        height = 200*(len(self.datfiles))
        writeln(f'set term png size 1000, {height}\n')
        writeln(f"set output '{self.png_filename}")
        writeln(f'set multiplot layout {len(self.datfiles)},1 margins 0.05,0.9, 0.05,0.9 spacing screen 0, char 5')
        writeln('set timefmt "%s"')
        writeln('set format x "%H:%M:%S"')
        writeln('set xlabel "Time"')
        writeln('set xdata time')
        writeln('set yrange [0:*]')
        for key, datfile in self.datfiles.items():
            datfile.file.flush()
            title = key.replace('_', '\\_')
            # linestyle 7 = red; lw => line weight
            writeln(f"plot '{datfile.file.name}' using 1:2 with lines linestyle 7 lw 4 title '{title}'")
        writeln('unset multiplot')

        outfile.close()
        #proc = subprocess.run(["/usr/bin/gnuplot", outfile.file.name], check=True, capture_output=True)
        proc = subprocess.run(["/usr/bin/gnuplot", outfile.name], check=True, capture_output=True)
        if proc.returncode != 0:
            print(proc)


def main():
    ftdc_filename = sys.argv[-1]
    if not ftdc_filename.endswith('.ftdc'):
        print("Last argument should be a `.ftdc` file. E.g: `python parse_ftdc.py ./diagnostics/viam-server.ftdc`")
        return

    ftdc_file = str(open(ftdc_filename).read())
    render = True
    render_options = {}
    mn_time = 0
    mx_time = 0
    while True:
        if render:
            plots = GnuPlotWriter()
            decoder = JSONDecoder()
            idx = 0
            while True:
                try:
                    obj, subIdx = decoder.raw_decode(ftdc_file[idx:])
                    if not obj:
                        break
                except:
                    break
                idx += subIdx + 1 # +1 for newline

                if not mn_time and not mx_time:
                    plots.play(obj)
                elif mn_time <= obj["Time"] and obj["Time"] <= mx_time:
                    plots.play(obj)

            plots.plot(render_options)
        try:
            '''
            Commands to add:
            - Hide/Show "all zero" charts
            - "pin" charts to top. Use glob patterns?
            - Highlight a time. Draw vertical line on each chart.
            '''
            render = True
            cmd = input('$ ')
            if cmd == 'quit':
                return
            elif cmd == 'h' or cmd == 'help':
                render = False
                print('range <start> <end>')
                print('-  Only plot datapoints within the given range. "zoom in"')
                print('-  E.g: range 2024-09-24T18:00:00 2024-09-24T18:30:00')
                print('-  All times in UTC')
                print()
                print('reset range')
                print('-  Unset any prior range. "zoom out to full"')
                print()
                print('`quit` or Ctrl-d to exit')
                pass
            elif cmd.startswith('range'):
                try:
                    _, start, end = cmd.split()
                    # Append +0000 to start/end to treat input as UTC which matches the timezone in
                    # the gnuplot images.
                    mn_time = datetime.datetime.strptime(f'{start}+0000', '%Y-%m-%dT%H:%M:%S%z').timestamp()
                    mx_time = datetime.datetime.strptime(f'{end}+0000', '%Y-%m-%dT%H:%M:%S%z').timestamp()
                except Exception:
                    print('Bad range input. Example: `range 2024-09-24T18:00:00 2024-09-24T18:30:00`')
                    render = False
            elif cmd == 'reset range':
                mn_time = mx_time = None
            else:
                print(f"Unknown command: `{cmd}`")
                render = False
        except EOFError:
            print()
            return

if __name__ == '__main__':
    main()
