FROM python:3.13-alpine
RUN pip install --no-cache-dir exabgp==5.0.15
ENV exabgp_daemon_user=root exabgp_log_level=INFO
CMD ["exabgp", "/etc/exabgp/exabgp.conf"]
