## Types of power ups 

- movement speed 
- hp stuff
    - health potions
    - defense stat upgrade (?)
- damage 
    - different weapons
    - damage multipliers 
    - ammo? 

## Weapon Mechanics

### Data structure of a weapon 

```json
// weapon
{
    "weapon_name": string,
    "reload_time": int,
    "bullet_speed": int
}
```

```json
// weapon state
{
    "ammo_left": int,
    "shots_fired": int 
}
```

```json
// ammo
{
    "weapon_name": string,
    "amount": int
}
```

### Damage potion (KIV)

```json
{
    "time_limit": int,
    "damage_multipler": int
}
```

## Damange mechanics

Critical hit -> 1 in every X attacks are critical
Implemented per weapon


## Map Data structures

### Entities 

```json
```