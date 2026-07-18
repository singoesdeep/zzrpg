use std::collections::HashMap;
use tonic::{transport::Server, Request, Response, Status};

pub mod pb {
    tonic::include_proto!("zzstat");
}

use pb::stat_service_server::{StatService, StatServiceServer};
use pb::{CalculateStatsRequest, CalculateStatsResponse, StatModifier};

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

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = "[::1]:50051".parse()?;
    let stat_service = MyStatService::default();

    println!("Rust zzstat service listening on {}", addr);

    Server::builder()
        .add_service(StatServiceServer::new(stat_service))
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
}
