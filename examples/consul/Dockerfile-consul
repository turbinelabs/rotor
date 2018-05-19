FROM consul:latest

ADD ./cow.json /etc/consul.d/cow.json
ADD ./horse.json /etc/consul.d/horse.json

EXPOSE 8500
EXPOSE 80

CMD ["agent", "-ui", "-dev", "-client", "0.0.0.0", "-config-dir", "/etc/consul.d"]
