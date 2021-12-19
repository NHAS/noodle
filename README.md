# noodle

A toy application level encryption module.  

A a high level, it provides confidentiality, integrity and availability and replay attack prevention. 

On a lower level it uses chacha20 with blake2s as hmac, with ed25519 and curve25519 for public key authentication. 

This also provides perfect forward secrecy through the use of ephmeral curve25519 keys that are generated on the fly with each new connection.