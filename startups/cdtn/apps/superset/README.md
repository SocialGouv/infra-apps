In superset pod, run this script to create the admin user

```sh
PKG_DIR=/tmp/py
export PYTHONPATH="$PKG_DIR:$PYTHONPATH"
pip install --no-cache-dir --target "$PKG_DIR" \
  "clickhouse-connect>=0.6.8" \
  "psycopg2-binary>=2.9.10"

export PIP_NO_CACHE_DIR=1

superset fab create-admin \
--username "admin" \
--firstname "Ad" \
--lastname "Min" \
--email "socialgroovybot@fabrique.social.gouv.fr" \
--password "xxx"
```
