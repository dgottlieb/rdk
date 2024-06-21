source ~/.viamdevrc

DIR=$PWD
cd ~/viam/rdk

make server-static

echo "SETTING CONFIG A. 100 ticks per rotation, sync on, 0.1 interval, capturedir"
./viam-cli robot part set-config --machine=03ea1870-89c3-4e47-9666-1ab65c92eb72 --part=a2ebaed3-71fd-4c1f-a0f2-9745140f7db7 --configFile=${DIR}/local_robot_A.json
sleep 1
./viam-server -config ${DIR}/robot.json &

sleep 5
echo "SETTING CONFIG B. 1000 ticks per rotation, sync off, 0.2 interval, capturedir2"
./viam-cli robot part set-config --machine=03ea1870-89c3-4e47-9666-1ab65c92eb72 --part=a2ebaed3-71fd-4c1f-a0f2-9745140f7db7 --configFile=${DIR}/local_robot_B.json
sleep 5
echo "SETTING CONFIG C. 1000 ticks per rotation, sync on, sync threads 50, 0.2 interval, capturedir2"
./viam-cli robot part set-config --machine=03ea1870-89c3-4e47-9666-1ab65c92eb72 --part=a2ebaed3-71fd-4c1f-a0f2-9745140f7db7 --configFile=${DIR}/local_robot_C.json
sleep 5
echo "SETTING CONFIG D. 100 ticks per rotation, sync on, sync threads 150, 0.1 interval, capturedir3"
./viam-cli robot part set-config --machine=03ea1870-89c3-4e47-9666-1ab65c92eb72 --part=a2ebaed3-71fd-4c1f-a0f2-9745140f7db7 --configFile=${DIR}/local_robot_D.json
sleep 5

kill %1
