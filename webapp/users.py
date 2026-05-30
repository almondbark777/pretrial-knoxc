"""
Allowed demo users for the shared-password access gate.

Usernames are matched case-insensitively; passwords are a single shared
secret in the APP_PASSWORD environment variable.  This is a TEMPORARY
access layer for the demo phase — real deployment should use Entra /
Microsoft 365 SSO with individual account control.
"""

_RAW_USERS = [
    "Daniel.Harris@knoxsheriff.org",
    "Justin.Webber@knoxsheriff.org",
    "Bryan.Hackett@knoxsheriff.org",
    "natashja.akers@knoxsheriff.org",
    "shellie.medford@knoxsheriff.org",
    "James.Rexroad@knoxsheriff.org",
    "james.alley@knoxsheriff.org",
    "Nicholas.Loveless@knoxsheriff.org",
    "William.Dunaway@knoxsheriff.org",
    "Marcus.Olsen@knoxsheriff.org",
    "Renee.Russell@knoxsheriff.org",
    "robert.burleson@knoxsheriff.org",
    "william.torbett@knoxsheriff.org",
    "Carla.Kidwell@knoxsheriff.org",
    "Kathy.Jones@knoxsheriff.org",
    "chloe.fudge@knoxsheriff.org",
    "Donna.Ogle@knoxsheriff.org",
    "Tyler.Rickman@knoxsheriff.org",
    "Stoney.Gentry@knoxsheriff.org",
    "amy.arroyo@knoxsheriff.org",
    "Johnie.Carter@knoxsheriff.org",
    "alexander.bentley@knoxsheriff.org",
]

# Frozen lowercase set for fast, case-insensitive membership checks.
ALLOWED_USERS = frozenset(u.strip().lower() for u in _RAW_USERS)
