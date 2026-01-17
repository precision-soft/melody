#!/bin/bash

./dc logs -f --tail=${TAIL:-100} "$@"
