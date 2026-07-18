use std::collections::HashMap;
use tonic::{transport::Server, Request, Response, Status};

pub mod pb {
    tonic::include_proto!("zzstat");
}

use pb::combat_service_server::{CombatService, CombatServiceServer};
use pb::stat_service_server::{StatService, StatServiceServer};
use pb::{
    CalculateDamageRequest, CalculateDamageResponse, CalculateStatsRequest, CalculateStatsResponse,
    CombatStats, StatModifier,
};

#[derive(Debug, Default)]
pub struct MyStatService {}

#[tonic::async_trait]
impl StatService for MyStatService {
    async fn calculate_stats(
        &self,
        request: Request<CalculateStatsRequest>,
    ) -> Result<Response<CalculateStatsResponse>, Status> {
        let req = request.into_inner();
        let mut final_stats = HashMap::new();

        // 1. Group modifiers by stat target
        let mut modifiers_by_stat: HashMap<String, Vec<StatModifier>> = HashMap::new();
        for m in req.modifiers {
            modifiers_by_stat
                .entry(m.stat.clone())
                .or_insert_with(Vec::new)
                .push(m);
        }

        // 2. Calculate primary attributes (STR, INT, DEX, CON)
        let primary_stats = vec!["STR", "INT", "DEX", "CON"];
        let mut calculated_primaries = HashMap::new();

        for prim in primary_stats {
            let mut val = 0.0;
            if let Some(mods) = modifiers_by_stat.get(prim) {
                // Sort by priority to ensure base setup runs first
                let mut sorted_mods = mods.clone();
                sorted_mods.sort_by_key(|m| m.priority);

                let mut add_sum = 0.0;
                let mut mult_sum = 0.0;

                for m in sorted_mods {
                    match m.operation.as_str() {
                        "ADD" => add_sum += m.value,
                        "MULTIPLY" => mult_sum += m.value,
                        _ => {}
                    }
                }
                val = add_sum * (1.0 + mult_sum);
            }
            calculated_primaries.insert(prim.to_string(), val);
            final_stats.insert(prim.to_string(), val);
        }

        // Extract primary values for formulas
        let str_val = *calculated_primaries.get("STR").unwrap_or(&0.0);
        let int_val = *calculated_primaries.get("INT").unwrap_or(&0.0);
        let dex_val = *calculated_primaries.get("DEX").unwrap_or(&0.0);
        let con_val = *calculated_primaries.get("CON").unwrap_or(&0.0);

        // 3. Compute base derived attributes
        let base_hp = con_val * 15.0;
        let base_mp = int_val * 10.0;
        let base_attack = str_val * 2.0 + dex_val * 0.5;
        let base_defense = con_val * 1.0 + str_val * 0.2;
        let base_crit = 5.0; // 5% base crit rate

        let derived_bases = vec![
            ("HP", base_hp),
            ("MP", base_mp),
            ("ATTACK", base_attack),
            ("DEFENSE", base_defense),
            ("CRIT_RATE", base_crit),
        ];

        // 4. Calculate final values for each derived attribute
        for (stat_name, base_val) in derived_bases {
            let mut flat_add = 0.0;
            let mut multiplier_sum = 0.0;

            if let Some(mods) = modifiers_by_stat.get(stat_name) {
                for m in mods {
                    match m.operation.as_str() {
                        "ADD" => flat_add += m.value,
                        "MULTIPLY" => multiplier_sum += m.value,
                        _ => {}
                    }
                }
            }

            let final_val = (base_val + flat_add) * (1.0 + multiplier_sum);
            final_stats.insert(stat_name.to_string(), final_val);
        }

        Ok(Response::new(CalculateStatsResponse { final_stats }))
    }
}

#[derive(Debug, Default)]
pub struct MyCombatService {}

#[tonic::async_trait]
impl CombatService for MyCombatService {
    async fn calculate_damage(
        &self,
        request: Request<CalculateDamageRequest>,
    ) -> Result<Response<CalculateDamageResponse>, Status> {
        let req = request.into_inner();
        let attacker = match req.attacker {
            Some(a) => a,
            None => return Err(Status::invalid_argument("missing attacker stats")),
        };
        let defender = match req.defender {
            Some(d) => d,
            None => return Err(Status::invalid_argument("missing defender stats")),
        };

        // 1. Calculate accuracy and check hit/miss
        // Dodge Rate = (Defender DEX * 0.2 + Defender Dodge Modifiers) / (Attacker Level * 1.5)
        let dodge_rate = (defender.dex * 0.2 + defender.dodge_modifiers) / (attacker.level as f64 * 1.5);
        let base_hit_chance = (1.0 - dodge_rate) + attacker.acc_modifiers;

        // Cap hit chance between 70% (0.70) and 99% (0.99)
        let hit_chance = base_hit_chance.clamp(0.70, 0.99);

        // Roll for hit
        let mut rng = rand::thread_rng();
        use rand::Rng;
        let hit_roll: f64 = rng.gen_range(0.0..1.0);
        if hit_roll >= hit_chance {
            // Miss! Return early
            return Ok(Response::new(CalculateDamageResponse {
                is_hit: false,
                damage: 0,
                is_crit: false,
            }));
        }

        // 2. Calculate base damage
        // If skill multiplier is non-zero, calculate skill damage: Base = Attacker Attack * SkillMultiplier + SkillFlatDamage.
        // Otherwise, Base = Attacker Attack.
        let base_dmg = if req.skill_multiplier > 0.0 {
            attacker.attack * req.skill_multiplier + req.skill_flat_damage
        } else {
            attacker.attack
        };

        // Damage = max(1, Base - Defender Defense)
        let mut damage = (base_dmg - defender.defense).max(1.0);

        // 3. Roll critical strike
        // Critical Strike: If roll < CRIT_RATE, Damage = Damage * 1.5 * (1 + CritDamageBonus)
        let crit_roll: f64 = rng.gen_range(0.0..100.0);
        let is_crit = crit_roll < attacker.crit_rate;
        if is_crit {
            damage = damage * 1.5 * (1.0 + attacker.crit_damage_bonus);
        }

        // 4. RNG Variance (±10%)
        // Final Damage = Damage * UniformRandom(0.9, 1.1)
        let variance: f64 = rng.gen_range(0.9..1.1);
        let final_damage = (damage * variance).round() as i32;

        Ok(Response::new(CalculateDamageResponse {
            is_hit: true,
            damage: final_damage.max(1),
            is_crit,
        }))
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = "0.0.0.0:50051".parse()?;
    let stat_service = MyStatService::default();
    let combat_service = MyCombatService::default();

    println!("Rust zzstat service listening on {}", addr);

    Server::builder()
        .add_service(StatServiceServer::new(stat_service))
        .add_service(CombatServiceServer::new(combat_service))
        .serve(addr)
        .await?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_calculate_stats() {
        let service = MyStatService::default();
        let request = Request::new(CalculateStatsRequest {
            character_id: 1,
            modifiers: vec![
                // Base CON = 10
                StatModifier {
                    stat: "CON".to_string(),
                    operation: "ADD".to_string(),
                    value: 10.0,
                    priority: 10,
                    source: "base".to_string(),
                    source_id: "base_con".to_string(),
                },
                // Sword flat Attack = +100
                StatModifier {
                    stat: "ATTACK".to_string(),
                    operation: "ADD".to_string(),
                    value: 100.0,
                    priority: 20,
                    source: "equipment".to_string(),
                    source_id: "sword".to_string(),
                },
                // Buff 20% Attack = +0.20
                StatModifier {
                    stat: "ATTACK".to_string(),
                    operation: "MULTIPLY".to_string(),
                    value: 0.20,
                    priority: 30,
                    source: "buff".to_string(),
                    source_id: "buff_atk".to_string(),
                },
            ],
        });

        let response = service.calculate_stats(request).await.unwrap().into_inner();
        let final_stats = response.final_stats;

        // Base CON = 10 -> Base HP = 150.0. No HP modifiers. Final HP = 150.0
        assert_eq!(*final_stats.get("HP").unwrap(), 150.0);

        // Base STR = 0, DEX = 0 -> Base Attack = 0.0
        // Attack modifiers: flat +100, multiply +0.20
        // Final Attack = (0.0 + 100.0) * (1.0 + 0.20) = 120.0
        assert_eq!(*final_stats.get("ATTACK").unwrap(), 120.0);
    }

    #[tokio::test]
    async fn test_calculate_damage_hit_and_crit() {
        let service = MyCombatService::default();

        // Attacker is level 10, Attack 150, Crit 100% (so we always crit)
        let attacker = CombatStats {
            level: 10,
            attack: 150.0,
            defense: 10.0,
            dex: 10.0,
            crit_rate: 100.0, // 100% critical rate
            crit_damage_bonus: 0.5, // +50% Crit Damage (so total 2.0x multiplier)
            acc_modifiers: 0.0,
            dodge_modifiers: 0.0,
        };

        // Defender has DEX 10, Defense 50
        let defender = CombatStats {
            level: 10,
            attack: 50.0,
            defense: 50.0,
            dex: 10.0,
            crit_rate: 5.0,
            crit_damage_bonus: 0.0,
            acc_modifiers: 0.0,
            dodge_modifiers: 0.0,
        };

        let request = Request::new(CalculateDamageRequest {
            attacker: Some(attacker),
            defender: Some(defender),
            skill_multiplier: 0.0,
            skill_flat_damage: 0.0,
        });

        let response = service.calculate_damage(request).await.unwrap().into_inner();

        // 1. Hit check:
        // Dodge Rate = (10 * 0.2 + 0) / (10 * 1.5) = 2.0 / 15.0 = 0.1333
        // Base Hit Chance = 1.0 - 0.1333 = 0.8667
        // Capped between 70% and 99%, so hit chance is 86.67%.
        // Wait, since hit chance is high, it will likely hit. Let's make sure it is true or false.
        // If it hit:
        if response.is_hit {
            // Normal base damage = max(1, 150 - 50) = 100.
            // Critical strike: 100% crit rate -> damage = 100 * 1.5 * (1 + 0.5) = 225.
            // Variance: 225 * RNG(0.9..1.1) -> damage must be between 202 and 248.
            assert!(response.is_crit);
            assert!(response.damage >= 202 && response.damage <= 248, "Damage was: {}", response.damage);
        }
    }
}
